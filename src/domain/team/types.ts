export interface Team {
  id: string
  name: string
  description: string
  ownerIds: string[]
  memberIds: string[]
  createdAt: string
  updatedAt: string
}

export type CreateTeamInput = Omit<Team, 'id' | 'createdAt' | 'updatedAt'>
export type UpdateTeamInput = Partial<Pick<Team, 'name' | 'description'>>
