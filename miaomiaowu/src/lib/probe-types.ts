export type ProbeType = 'nezha' | 'nezhav0' | 'dstatus' | 'komari' | 'cfsm'

export const PROBE_TYPES: { value: ProbeType; label: string }[] = [
  { value: 'nezha', label: '哪吒面板' },
  { value: 'nezhav0', label: '哪吒 V0' },
  { value: 'dstatus', label: 'DStatus' },
  { value: 'komari', label: 'Komari' },
  { value: 'cfsm', label: 'CF-Server-Monitor' },
]

type InventoryServer = { server_id: string; name: string }
type SavedServer = InventoryServer & {
  key: string
  traffic_method: string
  monthly_traffic_gb: number | string | null
  id?: number
}

// CF reports host usage, not the customer's purchased monthly allowance.
export function mergeCFSMInventory(
  inventory: InventoryServer[],
  existing: SavedServer[],
  generateKey: () => string
): SavedServer[] {
  return inventory.map((server) => {
    const previous = existing.find((entry) => entry.server_id === server.server_id)
    return {
      key: previous?.key ?? generateKey(),
      id: previous?.id,
      server_id: server.server_id,
      name: server.name,
      traffic_method: previous?.traffic_method ?? 'both',
      monthly_traffic_gb: previous?.monthly_traffic_gb ?? null,
    }
  })
}
