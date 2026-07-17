export interface Bot {
  id: number
  name: string
  owner: string
  language: string
  status: string
  createdAt: string
  rating: number | null
  wins: number | null
  losses: number | null
  draws: number | null
  matchesPlayed: number | null
}

export interface Match {
  id: number
  botAId: number
  botBId: number
  botAName: string
  botBName: string
  mapName: string
  status: string
  winnerId: number | null
  scoreA: number | null
  scoreB: number | null
  error: string | null
  createdAt: string
  finishedAt: string | null
  tournamentId: number | null
  round: number | null
  slot: number | null
}

export interface Tournament {
  id: number
  name: string
  format: string
  status: string
  mapName: string | null
  createdBy: string | null
  winnerId: number | null
  winnerName: string | null
  error: string | null
  participants: number
  matchesTotal: number
  matchesDone: number
  createdAt: string
  finishedAt: string | null
}

export interface TournamentParticipant {
  botId: number
  name: string
  owner: string
  language: string
  seed: number
  rating: number | null
}

export interface TournamentStanding {
  botId: number
  seed: number
  wins: number
  losses: number
  draws: number
  played: number
  points: number
  scoreDiff: number
}

export interface TournamentDetail {
  tournament: Tournament
  participants: TournamentParticipant[]
  matches: Match[]
  standings: TournamentStanding[] | null
}

export interface Turn {
  n: number
  player: number
  anfield: string
  piece: string
  x: number
  y: number
}

export interface Replay {
  match: Match
  turns: Turn[]
}

export interface Player {
  id: number
  name: string
  firstName: string | null
  lastName: string | null
  bots: number
  activeBots: number
  bestRating: number | null
  bestBot: string | null
  wins: number
  losses: number
  draws: number
  matchesPlayed: number
}

export interface AuthUser {
  id: number
  login: string
  firstName: string | null
  lastName: string | null
  email: string | null
  auditRatio: number | null
  isAdmin: boolean
}

export interface AdminOverview {
  queueBuilds: number
  queueMatches: number
  bots: Record<string, number>
  matches: Record<string, number>
  finished24h: number
  avgDurationSec: number | null
  players: number
}

export interface RatingSeries {
  botId: number
  botName: string
  points: { n: number; rating: number; at: string }[]
}

export interface PlayerStats {
  ratingHistory: RatingSeries[]
  perMap: { map: string; wins: number; losses: number; draws: number }[]
  nemesis: { name: string; owner: string; wins: number; losses: number } | null
  prey: { name: string; owner: string; wins: number; losses: number } | null
  streakCurrent: number
  streakBest: number
  domination: number | null
  activity: { day: string; count: number }[]
  totalMatches: number
}

export interface GameMap {
  id: number
  name: string
  width: number
  height: number
}

async function req<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, init)
  const body = await res.json().catch(() => null)
  if (!res.ok) {
    throw new Error(body?.error ?? `${res.status} ${res.statusText}`)
  }
  return body as T
}

export const api = {
  leaderboard: () => req<Bot[]>('/api/leaderboard'),
  bots: () => req<Bot[]>('/api/bots'),
  bot: (id: number | string) =>
    req<{ bot: Bot; buildLog: string | null; matches: Match[] }>(`/api/bots/${id}`),
  matches: () => req<Match[]>('/api/matches'),
  replay: (id: number | string) => req<Replay>(`/api/matches/${id}/replay`),
  maps: () => req<GameMap[]>('/api/maps'),
  players: () => req<Player[]>('/api/players'),
  player: (name: string) =>
    req<{ player: Player; bots: Bot[]; matches: Match[] }>(`/api/players/${encodeURIComponent(name)}`),
  playerStats: (name: string) =>
    req<PlayerStats>(`/api/players/${encodeURIComponent(name)}/stats`),
  me: () => req<{ user: AuthUser | null }>('/api/auth/me'),
  login: (identifier: string, password: string) =>
    req<AuthUser>('/api/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ identifier, password }),
    }),
  logout: () => req<{ ok: boolean }>('/api/auth/logout', { method: 'POST' }),
  adminOverview: () => req<AdminOverview>('/api/admin/overview'),
  adminRequeue: (matchId: number) =>
    req<{ id: number }>(`/api/admin/matches/${matchId}/requeue`, { method: 'POST' }),
  adminSetBotStatus: (botId: number, status: 'active' | 'inactive') =>
    req<{ id: number }>(`/api/admin/bots/${botId}/status`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ status }),
    }),
  adminDeleteBot: (botId: number) =>
    req<{ deleted: number }>(`/api/admin/bots/${botId}`, { method: 'DELETE' }),
  uploadBot: (form: FormData) =>
    req<{ id: number; status: string }>('/api/bots', { method: 'POST', body: form }),
  tournaments: () => req<Tournament[]>('/api/tournaments'),
  tournament: (id: number | string) => req<TournamentDetail>(`/api/tournaments/${id}`),
  createTournament: (body: { name: string; format: string; botIds: number[]; mapId: number }) =>
    req<{ id: number; status: string }>('/api/tournaments', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }),
  createMatch: (botAId: number, botBId: number, mapId?: number) =>
    req<{ id: number; status: string }>('/api/matches', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ botAId, botBId, mapId: mapId ?? 0 }),
    }),
}
