import type { Datacenter, Room, Rack } from '@/domain/topology/types'

const daysAgo = (d: number) => new Date(Date.now() - d * 86400 * 1000).toISOString()

export const seedDatacenters: Datacenter[] = [
  {
    id: 'dc-east',
    name: 'DC-East',
    code: 'DCE',
    location: 'Virginia, US',
    description: 'Primary east coast datacenter for GPU compute workloads',
    createdAt: daysAgo(180),
    updatedAt: daysAgo(10),
  },
  {
    id: 'dc-west',
    name: 'DC-West',
    code: 'DCW',
    location: 'Oregon, US',
    description: 'West coast secondary datacenter for redundancy and inference',
    createdAt: daysAgo(120),
    updatedAt: daysAgo(5),
  },
]

export const seedRooms: Room[] = [
  {
    id: 'room-east-a',
    datacenterId: 'dc-east',
    name: 'Room-A',
    floor: '1F',
    description: 'GPU compute cluster room A',
    createdAt: daysAgo(170),
    updatedAt: daysAgo(30),
  },
  {
    id: 'room-east-b',
    datacenterId: 'dc-east',
    name: 'Room-B',
    floor: '2F',
    description: 'Storage and networking room',
    createdAt: daysAgo(160),
    updatedAt: daysAgo(20),
  },
  {
    id: 'room-west-a',
    datacenterId: 'dc-west',
    name: 'Room-A',
    floor: '1F',
    description: 'Inference cluster room',
    createdAt: daysAgo(110),
    updatedAt: daysAgo(7),
  },
]

export const seedRacks: Rack[] = [
  {
    id: 'rack-east-r01',
    datacenterId: 'dc-east',
    roomId: 'room-east-a',
    name: 'R01',
    rackUnit: 42,
    description: 'Primary GPU server rack',
    createdAt: daysAgo(165),
    updatedAt: daysAgo(15),
  },
  {
    id: 'rack-east-r02',
    datacenterId: 'dc-east',
    roomId: 'room-east-a',
    name: 'R02',
    rackUnit: 42,
    description: 'Secondary GPU server rack',
    createdAt: daysAgo(165),
    updatedAt: daysAgo(15),
  },
  {
    id: 'rack-east-r04',
    datacenterId: 'dc-east',
    roomId: 'room-east-b',
    name: 'R04',
    rackUnit: 48,
    description: 'Storage rack',
    createdAt: daysAgo(155),
    updatedAt: daysAgo(25),
  },
  {
    id: 'rack-east-rx',
    datacenterId: 'dc-east',
    roomId: null,
    name: 'RX-01',
    rackUnit: 24,
    description: 'Standalone rack without dedicated room assignment',
    createdAt: daysAgo(90),
    updatedAt: daysAgo(3),
  },
  {
    id: 'rack-west-r01',
    datacenterId: 'dc-west',
    roomId: 'room-west-a',
    name: 'R01',
    rackUnit: 42,
    description: 'Inference server rack',
    createdAt: daysAgo(105),
    updatedAt: daysAgo(6),
  },
]
