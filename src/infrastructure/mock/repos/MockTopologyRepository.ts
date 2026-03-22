import type {
  Datacenter, CreateDatacenterInput, UpdateDatacenterInput,
  Room, CreateRoomInput, UpdateRoomInput,
  Rack, CreateRackInput, UpdateRackInput,
} from '@/domain/topology/types'
import type { TopologyRepository, ListRoomsFilters, ListRacksFilters } from '@/application/ports/TopologyRepository'
import { seedDatacenters, seedRooms, seedRacks } from '@/infrastructure/mock/data/seedTopology'
import { lsGet, lsSet } from '@/infrastructure/persistence/localStorage'

const LS_DC = 'topology-datacenters'
const LS_ROOMS = 'topology-rooms'
const LS_RACKS = 'topology-racks'

function genId(prefix: string) {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 5)}`
}

function now() {
  return new Date().toISOString()
}

export class MockTopologyRepository implements TopologyRepository {
  private dcs: Map<string, Datacenter>
  private rooms: Map<string, Room>
  private racks: Map<string, Rack>

  constructor() {
    const storedDcs = lsGet<Datacenter[]>(LS_DC, seedDatacenters)
    const storedRooms = lsGet<Room[]>(LS_ROOMS, seedRooms)
    const storedRacks = lsGet<Rack[]>(LS_RACKS, seedRacks)
    this.dcs = new Map(storedDcs.map((d) => [d.id, d]))
    this.rooms = new Map(storedRooms.map((r) => [r.id, r]))
    this.racks = new Map(storedRacks.map((r) => [r.id, r]))
  }

  private persistDcs() { lsSet(LS_DC, Array.from(this.dcs.values())) }
  private persistRooms() { lsSet(LS_ROOMS, Array.from(this.rooms.values())) }
  private persistRacks() { lsSet(LS_RACKS, Array.from(this.racks.values())) }

  async listDatacenters(): Promise<Datacenter[]> {
    return Array.from(this.dcs.values()).sort((a, b) => a.name.localeCompare(b.name))
  }

  async getDatacenter(id: string): Promise<Datacenter | null> {
    return this.dcs.get(id) ?? null
  }

  async createDatacenter(input: CreateDatacenterInput): Promise<Datacenter> {
    const dc: Datacenter = { ...input, id: genId('dc'), createdAt: now(), updatedAt: now() }
    this.dcs.set(dc.id, dc)
    this.persistDcs()
    return dc
  }

  async updateDatacenter(id: string, input: UpdateDatacenterInput): Promise<Datacenter> {
    const existing = this.dcs.get(id)
    if (!existing) throw new Error(`Datacenter not found: ${id}`)
    const updated: Datacenter = { ...existing, ...input, updatedAt: now() }
    this.dcs.set(id, updated)
    this.persistDcs()
    return updated
  }

  async deleteDatacenter(id: string): Promise<void> {
    if (!this.dcs.has(id)) throw new Error(`Datacenter not found: ${id}`)
    const hasRooms = Array.from(this.rooms.values()).some((r) => r.datacenterId === id)
    if (hasRooms) throw new Error('Cannot delete datacenter: it still has rooms. Remove all rooms first.')
    const hasRacks = Array.from(this.racks.values()).some((r) => r.datacenterId === id)
    if (hasRacks) throw new Error('Cannot delete datacenter: it still has racks. Remove all racks first.')
    this.dcs.delete(id)
    this.persistDcs()
  }

  async listRooms(filters?: ListRoomsFilters): Promise<Room[]> {
    let result = Array.from(this.rooms.values())
    if (filters?.datacenterId) {
      result = result.filter((r) => r.datacenterId === filters.datacenterId)
    }
    return result.sort((a, b) => a.name.localeCompare(b.name))
  }

  async getRoom(id: string): Promise<Room | null> {
    return this.rooms.get(id) ?? null
  }

  async createRoom(input: CreateRoomInput): Promise<Room> {
    const room: Room = { ...input, id: genId('room'), createdAt: now(), updatedAt: now() }
    this.rooms.set(room.id, room)
    this.persistRooms()
    return room
  }

  async updateRoom(id: string, input: UpdateRoomInput): Promise<Room> {
    const existing = this.rooms.get(id)
    if (!existing) throw new Error(`Room not found: ${id}`)
    const updated: Room = { ...existing, ...input, updatedAt: now() }
    this.rooms.set(id, updated)
    this.persistRooms()
    return updated
  }

  async deleteRoom(id: string): Promise<void> {
    if (!this.rooms.has(id)) throw new Error(`Room not found: ${id}`)
    const hasRacks = Array.from(this.racks.values()).some((r) => r.roomId === id)
    if (hasRacks) throw new Error('Cannot delete room: it still has racks. Remove all racks first.')
    this.rooms.delete(id)
    this.persistRooms()
  }

  async listRacks(filters?: ListRacksFilters): Promise<Rack[]> {
    let result = Array.from(this.racks.values())
    if (filters?.datacenterId) {
      result = result.filter((r) => r.datacenterId === filters.datacenterId)
    }
    if (filters?.roomId !== undefined) {
      result = result.filter((r) => r.roomId === filters.roomId)
    }
    return result.sort((a, b) => a.name.localeCompare(b.name))
  }

  async getRack(id: string): Promise<Rack | null> {
    return this.racks.get(id) ?? null
  }

  async createRack(input: CreateRackInput): Promise<Rack> {
    const rack: Rack = { ...input, id: genId('rack'), createdAt: now(), updatedAt: now() }
    this.racks.set(rack.id, rack)
    this.persistRacks()
    return rack
  }

  async updateRack(id: string, input: UpdateRackInput): Promise<Rack> {
    const existing = this.racks.get(id)
    if (!existing) throw new Error(`Rack not found: ${id}`)
    const updated: Rack = { ...existing, ...input, updatedAt: now() }
    this.racks.set(id, updated)
    this.persistRacks()
    return updated
  }

  async deleteRack(id: string): Promise<void> {
    if (!this.racks.has(id)) throw new Error(`Rack not found: ${id}`)
    this.racks.delete(id)
    this.persistRacks()
  }
}
