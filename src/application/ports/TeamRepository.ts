import type { Team, CreateTeamInput, UpdateTeamInput } from '@/domain/team/types'

export interface TeamRepository {
  listTeams(): Promise<Team[]>
  getTeam(id: string): Promise<Team | null>
  createTeam(input: CreateTeamInput): Promise<Team>
  updateTeam(id: string, input: UpdateTeamInput): Promise<Team>
  deleteTeam(id: string): Promise<void>
  addMember(teamId: string, userId: string): Promise<Team>
  removeMember(teamId: string, userId: string): Promise<Team>
  addOwner(teamId: string, userId: string): Promise<Team>
  removeOwner(teamId: string, userId: string): Promise<Team>
}
