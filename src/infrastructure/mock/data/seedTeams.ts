import type { Team } from '@/domain/team/types'

const now = new Date().toISOString()
const daysAgo = (d: number) => new Date(Date.now() - d * 86400 * 1000).toISOString()

export const seedTeams: Team[] = [
  {
    id: 'team-ai',
    name: 'AI Team',
    description: 'Large-scale model training and inference workloads',
    ownerIds: ['user-admin'],
    memberIds: ['user-admin', 'user-owner'],
    createdAt: daysAgo(30),
    updatedAt: daysAgo(5),
  },
  {
    id: 'team-infra',
    name: 'Infra Team',
    description: 'Platform infrastructure, networking, and storage management',
    ownerIds: ['user-owner'],
    memberIds: ['user-owner', 'user-member'],
    createdAt: daysAgo(60),
    updatedAt: daysAgo(10),
  },
  {
    id: 'team-research',
    name: 'Research Team',
    description: 'Experimental GPU workloads and research projects',
    ownerIds: ['user-admin'],
    memberIds: ['user-admin', 'user-member'],
    createdAt: daysAgo(14),
    updatedAt: now,
  },
]
