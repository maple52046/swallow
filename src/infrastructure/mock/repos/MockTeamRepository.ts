import type { Team, CreateTeamInput, UpdateTeamInput } from '@/domain/team/types'
import type { TeamRepository } from '@/application/ports/TeamRepository'
import { seedTeams } from '@/infrastructure/mock/data/seedTeams'
import { lsGet, lsSet } from '@/infrastructure/persistence/localStorage'

const LS_KEY = 'teams'

function genId() {
  return 'team-' + Date.now().toString(36) + Math.random().toString(36).slice(2, 5)
}

function now() {
  return new Date().toISOString()
}

export class MockTeamRepository implements TeamRepository {
  private teams: Map<string, Team>

  constructor() {
    const stored = lsGet<Team[]>(LS_KEY, seedTeams)
    this.teams = new Map(stored.map((t) => [t.id, t]))
  }

  private persist() {
    lsSet(LS_KEY, Array.from(this.teams.values()))
  }

  async listTeams(): Promise<Team[]> {
    return Array.from(this.teams.values()).sort((a, b) => a.name.localeCompare(b.name))
  }

  async getTeam(id: string): Promise<Team | null> {
    return this.teams.get(id) ?? null
  }

  async createTeam(input: CreateTeamInput): Promise<Team> {
    const team: Team = {
      ...input,
      id: genId(),
      createdAt: now(),
      updatedAt: now(),
    }
    this.teams.set(team.id, team)
    this.persist()
    return team
  }

  async updateTeam(id: string, input: UpdateTeamInput): Promise<Team> {
    const existing = this.teams.get(id)
    if (!existing) throw new Error(`Team not found: ${id}`)
    const updated: Team = { ...existing, ...input, updatedAt: now() }
    this.teams.set(id, updated)
    this.persist()
    return updated
  }

  async deleteTeam(id: string): Promise<void> {
    if (!this.teams.has(id)) throw new Error(`Team not found: ${id}`)
    this.teams.delete(id)
    this.persist()
  }

  async addMember(teamId: string, userId: string): Promise<Team> {
    const team = this.teams.get(teamId)
    if (!team) throw new Error(`Team not found: ${teamId}`)
    if (team.memberIds.includes(userId)) return team
    const updated: Team = { ...team, memberIds: [...team.memberIds, userId], updatedAt: now() }
    this.teams.set(teamId, updated)
    this.persist()
    return updated
  }

  async removeMember(teamId: string, userId: string): Promise<Team> {
    const team = this.teams.get(teamId)
    if (!team) throw new Error(`Team not found: ${teamId}`)
    const updated: Team = {
      ...team,
      memberIds: team.memberIds.filter((id) => id !== userId),
      ownerIds: team.ownerIds.filter((id) => id !== userId),
      updatedAt: now(),
    }
    if (updated.ownerIds.length === 0) {
      throw new Error('Team must have at least one owner')
    }
    this.teams.set(teamId, updated)
    this.persist()
    return updated
  }

  async addOwner(teamId: string, userId: string): Promise<Team> {
    const team = this.teams.get(teamId)
    if (!team) throw new Error(`Team not found: ${teamId}`)
    const memberIds = team.memberIds.includes(userId) ? team.memberIds : [...team.memberIds, userId]
    const ownerIds = team.ownerIds.includes(userId) ? team.ownerIds : [...team.ownerIds, userId]
    const updated: Team = { ...team, ownerIds, memberIds, updatedAt: now() }
    this.teams.set(teamId, updated)
    this.persist()
    return updated
  }

  async removeOwner(teamId: string, userId: string): Promise<Team> {
    const team = this.teams.get(teamId)
    if (!team) throw new Error(`Team not found: ${teamId}`)
    const ownerIds = team.ownerIds.filter((id) => id !== userId)
    if (ownerIds.length === 0) throw new Error('Team must have at least one owner')
    const updated: Team = { ...team, ownerIds, updatedAt: now() }
    this.teams.set(teamId, updated)
    this.persist()
    return updated
  }
}
