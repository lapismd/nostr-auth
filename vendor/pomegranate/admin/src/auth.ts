import { getTokenCreatedAt } from "./utils"

const TOKEN_STORAGE_KEY = "token"
const CENTRAL_STORAGE_KEY = "centralURL"
const TOKEN_MAX_AGE_MS = 24 * 60 * 60 * 1000

export function authenticate(centralURL: string): Promise<string> {
  return new Promise((resolve, reject) => {
    window.addEventListener("message", handler)

    const popup = window.open(`${centralURL}/login/google`, "OAuth", "width=600,height=600")
    if (!popup) {
      reject(new Error("popup blocked"))
      return
    }

    function handler(event: MessageEvent) {
      if (event.origin !== centralURL || !event.data.token) return

      window.removeEventListener("message", handler)
      storeAuth(event.data.token, centralURL)
      resolve(event.data.token)
      popup!.close()
    }
  })
}

export function storeAuth(token: string, centralURL: string) {
  localStorage.setItem(TOKEN_STORAGE_KEY, token)
  localStorage.setItem(CENTRAL_STORAGE_KEY, centralURL)
}

export function clearStoredToken() {
  localStorage.removeItem(TOKEN_STORAGE_KEY)
  localStorage.removeItem(CENTRAL_STORAGE_KEY)
}

export function getStoredAuth(): { token: string; centralURL: string } | null {
  const token = localStorage.getItem(TOKEN_STORAGE_KEY)
  const centralURL = localStorage.getItem(CENTRAL_STORAGE_KEY)
  if (!token || !centralURL) {
    clearStoredToken()
    return null
  }

  const createdAt = getTokenCreatedAt(token)
  if (createdAt === null || Date.now() - createdAt > TOKEN_MAX_AGE_MS) {
    clearStoredToken()
    return null
  }

  return { token, centralURL }
}
