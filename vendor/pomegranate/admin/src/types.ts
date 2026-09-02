export type AccountOperator = {
  url: string
  pubshard: string
}

export type Account = {
  email: string
  pubkey: string
  operators: AccountOperator[]
  threshold: number
}

export type ExistingSetupAnnouncement = {
  centralURL: string
  createdAt: number
}

export type ProfileFilter = {
  kinds?: number[]
  until?: number
}

export type Profile = {
  handler_pubkey: string
  name: string
  email: string
}
