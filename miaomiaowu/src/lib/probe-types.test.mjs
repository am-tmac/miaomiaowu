import assert from 'node:assert/strict'
import test from 'node:test'
import { PROBE_TYPES, mergeCFSMInventory } from './probe-types.ts'

test('CF-Server-Monitor is selectable', () => {
  assert.deepEqual(PROBE_TYPES.find((p) => p.value === 'cfsm'), { value: 'cfsm', label: 'CF-Server-Monitor' })
})
test('CFSM inventory keeps UUID and user allowance/mode without inventing limits', () => {
 const existing = [{ key: 'keep', server_id: 'uuid-a', name: 'old', monthly_traffic_gb: 123, traffic_method: 'up' }]
 const result = mergeCFSMInventory([{ server_id: 'uuid-a', name: 'new' }, { server_id: 'uuid-b', name: 'second', monthly_traffic_gb: 999 }], existing, () => 'new-key')
 assert.equal(result[0].server_id, 'uuid-a')
 assert.equal(result[0].name, 'new')
 assert.equal(result[0].monthly_traffic_gb, 123)
 assert.equal(result[0].traffic_method, 'up')
 assert.equal(result[1].monthly_traffic_gb, null)
 assert.equal(result[1].traffic_method, 'both')
 assert.equal(existing[0].name, 'old')
})
