export interface Datacenter {
  id: string
  name: string
  code?: string
  location?: string
  description?: string
  createdAt: string
  updatedAt: string
}

export interface Room {
  id: string
  datacenterId: string
  name: string
  floor?: string
  description?: string
  createdAt: string
  updatedAt: string
}

export interface Rack {
  id: string
  datacenterId: string
  roomId: string | null
  name: string
  rackUnit?: number
  description?: string
  createdAt: string
  updatedAt: string
}

export type CreateDatacenterInput = Omit<Datacenter, 'id' | 'createdAt' | 'updatedAt'>
export type UpdateDatacenterInput = Partial<Pick<Datacenter, 'name' | 'code' | 'location' | 'description'>>

export type CreateRoomInput = Omit<Room, 'id' | 'createdAt' | 'updatedAt'>
export type UpdateRoomInput = Partial<Pick<Room, 'name' | 'floor' | 'description'>>

export type CreateRackInput = Omit<Rack, 'id' | 'createdAt' | 'updatedAt'>
export type UpdateRackInput = Partial<Pick<Rack, 'name' | 'roomId' | 'rackUnit' | 'description'>>
