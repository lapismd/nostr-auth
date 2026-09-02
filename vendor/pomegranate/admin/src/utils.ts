import { SimplePool } from "@nostr/tools"

export const pool = new SimplePool()

export function massageURL(input: string): string {
  let url = input.trim()
  if (!url.startsWith("http")) {
    url = "http" + (url.startsWith("localhost") ? "" : "s") + "://" + url
  }
  return new URL(url).origin
}

export function normalizeOperators(values: string[]) {
  const normalized = values.filter(
    (value, index) => index === 0 || !(value === "" && values[index - 1] === "")
  )

  if (normalized.length === 0) {
    return [""]
  }

  if (normalized[normalized.length - 1] !== "") {
    normalized.push("")
  }

  return normalized
}

export function formatTimestamp(timestamp: number) {
  return new Date(timestamp * 1000).toLocaleString()
}

export function getFirstTagValue(tags: string[][], name: string) {
  const tag = tags.find(tag => tag[0] === name && typeof tag[1] === "string")
  return tag?.[1] ?? null
}

export function getTokenCreatedAt(token: string) {
  try {
    const decoded = atob(token)
    const parsed = JSON.parse(decoded) as { created_at?: unknown }
    return typeof parsed.created_at === "number" ? parsed.created_at * 1000 : null
  } catch {
    return null
  }
}

export function getTokenEmail(token: string) {
  try {
    const decoded = atob(token)
    const parsed = JSON.parse(decoded) as { tags?: unknown }
    if (!Array.isArray(parsed.tags)) {
      return ""
    }

    const emailTag = parsed.tags.find(
      (tag): tag is [string, string] =>
        Array.isArray(tag) && tag.length > 1 && tag[0] === "email" && typeof tag[1] === "string"
    )

    return emailTag?.[1] ?? ""
  } catch {
    return ""
  }
}
