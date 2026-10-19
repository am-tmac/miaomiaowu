package handler

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"time"

	"miaomiaowu/internal/auth"
	"miaomiaowu/internal/captcha"
	"miaomiaowu/internal/logger"
	"miaomiaowu/internal/storage"
)

type loginRequest struct {
	Username       string `json:"username"`
	Password       string `json:"password"`
	RememberMe     bool   `json:"remember_me"`
	TurnstileToken string `json:"turnstile_token"`
}

type loginResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	Nickname  string    `json:"nickname"`
	Avatar    string    `json:"avatar_url"`
	Role      string    `json:"role"`
	IsAdmin   bool      `json:"is_admin"`
}

type credentialsRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// GetClientIP uses forwarding headers only from explicitly trusted socket peers.
// TRUSTED_PROXIES is a comma-separated list of literal IPs/CIDRs; an empty or
// invalid list trusts nobody. CF-Connecting-IP is deliberately never consumed:
// ordinary trusted proxies may pass that client-controlled header unchanged.
func GetClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil || peer.Zone() != "" {
		return ""
	}
	peer = peer.Unmap()
	fallback := peer.String()

	var proxies []netip.Prefix
	if raw := strings.TrimSpace(os.Getenv("TRUSTED_PROXIES")); raw != "" {
		for _, entry := range strings.Split(raw, ",") {
			entry = strings.TrimSpace(entry)
			prefix, err := netip.ParsePrefix(entry)
			if err != nil {
				ip, ipErr := netip.ParseAddr(entry)
				if ipErr != nil || ip.Zone() != "" {
					return fallback // Never partially accept malformed configuration.
				}
				ip = ip.Unmap()
				prefix = netip.PrefixFrom(ip, ip.BitLen())
			}
			proxies = append(proxies, prefix)
		}
	}
	trusted := func(ip netip.Addr) bool {
		for _, prefix := range proxies {
			if prefix.Contains(ip) {
				return true
			}
		}
		return false
	}
	if !trusted(peer) {
		return fallback
	}

	// Combine all XFF fields in wire order, then walk away from the socket
	// through trusted hops. Never select the attacker-controlled leftmost value.
	if values := r.Header.Values("X-Forwarded-For"); len(values) > 0 {
		hops := strings.Split(strings.Join(values, ","), ",")
		current := peer
		for i := len(hops) - 1; i >= 0; i-- {
			if !trusted(current) {
				return current.String()
			}
			next, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
			if err != nil || next.Zone() != "" {
				return fallback // Do not fall through to a weaker header.
			}
			current = next.Unmap()
		}
		return current.String()
	}
	// Single-IP compatibility fallback: the trusted proxy must overwrite X-Real-IP.
	if values := r.Header.Values("X-Real-IP"); len(values) == 1 {
		if ip, err := netip.ParseAddr(strings.TrimSpace(values[0])); err == nil && ip.Zone() == "" {
			return ip.Unmap().String()
		}
	}
	return fallback
}

func NewLoginHandler(manager *auth.Manager, tokens *auth.TokenStore, repo *storage.TrafficRepository, rateLimiter *LoginRateLimiter, twoFactorStore *auth.TwoFactorPendingStore, turnstile ...*captcha.Turnstile) http.Handler {
	if manager == nil || tokens == nil {
		panic("login handler requires manager and token store")
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, errors.New("only POST is supported"))
			return
		}

		var payload loginRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}

		if strings.TrimSpace(payload.Username) == "" || payload.Password == "" {
			writeError(w, http.StatusBadRequest, errors.New("username and password are required"))
			return
		}

		username := strings.TrimSpace(payload.Username)
		clientIP := GetClientIP(r)
		if len(turnstile) > 0 && turnstile[0] != nil && !turnstile[0].Verify(r.Context(), payload.TurnstileToken, clientIP) {
			writeError(w, http.StatusBadRequest, errors.New("captcha verification failed"))
			return
		}

		// 检查速率限制
		if rateLimiter != nil {
			if err := rateLimiter.Check(clientIP, username); err != nil {
				writeError(w, http.StatusTooManyRequests, errors.New("too many login attempts, please try again later"))
				return
			}
		}

		ok, err := manager.Authenticate(r.Context(), username, payload.Password)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		if !ok {
			// 记录登录失败
			if rateLimiter != nil {
				rateLimiter.RecordFailure(clientIP, username)
			}
			logger.Warn("🔐 [LOGIN_FAIL] 登录失败",
				"username", username,
				"client_ip", clientIP,
				"time", time.Now().Format("2006-01-02 15:04:05"))
			writeError(w, http.StatusUnauthorized, errors.New("invalid credentials"))
			return
		}

		// 登录成功，清除速率限制计数
		if rateLimiter != nil {
			rateLimiter.RecordSuccess(clientIP, username)
		}

		user, err := manager.User(r.Context(), username)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		if user.TOTPEnabled && twoFactorStore != nil {
			tfToken, err := twoFactorStore.Issue(username, payload.RememberMe)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"requires_2fa":     true,
				"two_factor_token": tfToken,
			})
			return
		}

		if repo != nil {
			if _, err := repo.GetOrCreateUserToken(r.Context(), username); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
		}

		issueLoginSession(w, r, tokens, repo, user, payload.RememberMe)
	})
}

func NewCredentialsHandler(manager *auth.Manager, tokens *auth.TokenStore) http.Handler {
	if manager == nil || tokens == nil {
		panic("credentials handler requires manager and token store")
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			writeError(w, http.StatusMethodNotAllowed, errors.New("only PUT is supported"))
			return
		}

		var payload credentialsRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}

		trimmedUsername := strings.TrimSpace(payload.Username)

		if trimmedUsername == "" && payload.Password == "" {
			writeError(w, http.StatusBadRequest, errors.New("username or password must be provided"))
			return
		}

		if err := manager.Update(r.Context(), trimmedUsername, payload.Password); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		tokens.RevokeAll()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
	})
}
