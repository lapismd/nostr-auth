import { Account, Profile } from "./types"

export async function getAccount(token: string, centralURL: string): Promise<Account | null> {
  const resp = await fetch(centralURL + "/account", {
    headers: { Authorization: "Token " + token }
  })

  if (resp.status === 401) return null
  if (!resp.ok) return null

  return (await resp.json()) as Account
}

export async function fetchProfiles(token: string, centralURL: string): Promise<Profile[]> {
  const resp = await fetch(centralURL + "/profiles", {
    headers: { Authorization: "Token " + token }
  })

  if (!resp.ok) throw new Error("failed to load profiles")

  return await resp.json()
}

export async function createProfile(
  token: string,
  centralURL: string,
  name: string,
  restrictions?: Record<string, unknown>
): Promise<void> {
  const resp = await fetch(centralURL + "/profiles", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: "Token " + token
    },
    body: JSON.stringify({ name, restrictions })
  })

  if (!resp.ok) throw new Error("profile creation failed")
}

export async function deleteProfile(token: string, centralURL: string, handlerPubkey: string): Promise<void> {
  const resp = await fetch(centralURL + `/profiles/${handlerPubkey}`, {
    method: "DELETE",
    headers: { Authorization: "Token " + token }
  })

  if (!resp.ok) throw new Error("profile deletion failed")
}

export async function resetAccount(token: string, centralURL: string): Promise<void> {
  const resp = await fetch(centralURL + "/account", {
    method: "DELETE",
    headers: { Authorization: "Token " + token }
  })

  if (!resp.ok) throw new Error("account reset failed")
}
