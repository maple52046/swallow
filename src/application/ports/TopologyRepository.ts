import type {
  Datacenter, CreateDatacenterInput, UpdateDatacenterInput,
  Room, CreateRoomInput, UpdateRoomInput,
  Rack, CreateRackInput, UpdateRackInput,
} from '@/domain/topology/types'

export interface ListRoomsFilters { datacenterId?: string }
export interface ListRacksFilters { datacenterId?: string; roomId?: string | null }

export interface TopologyRepository {
  listDatacenters(): Promise<Datacenter[]>
  getDatacenter(id: string): Promise<Datacenter | null>
  createDatacenter(input: CreateDatacenterInput): Promise<Datacenter>
  updateDatacenter(id: string, input: UpdateDatacenterInput): Promise<Datacenter>
  deleteDatacenter(id: string): Promise<void>

  listRooms(filters?: ListRoomsFilters): Promise<Room[]>
  getRoom(id: string): Promise<Room | null>
  createRoom(input: CreateRoomInput): Promise<Room>
  updateRoom(id: string, input: UpdateRoomInput): Promise<Room>
  deleteRoom(id: string): Promise<void>

  listRacks(filters?: ListRacksFilters): Promise<Rack[]>
  getRack(id: string): Promise<Rack | null>
  createRack(input: CreateRackInput): Promise<Rack>
  updateRack(id: string, input: UpdateRackInput): Promise<Rack>
  deleteRack(id: string): Promise<void>
}
