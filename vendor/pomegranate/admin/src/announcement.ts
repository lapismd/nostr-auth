import { finalizeEvent } from "@nostr/tools"
import { argon2id } from "@noble/hashes/argon2.js"
import { bytesToHex } from "@noble/hashes/utils.js"
import { Relay } from "@nostr/tools/relay"
import { getFirstTagValue, massageURL, pool } from "./utils"
import type { Account } from "./types"

const utf8 = new TextEncoder()
const SETUP_ANNOUNCEMENT_RELAYS = [
  "wss://relay.damus.io",
  "wss://relay.primal.net",
  "wss://nos.lol",
  "wss://nostr.mom",
  "wss://offchain.pub"
]
const KIND_SETUP_ANNOUNCEMENT = 16440

async function publishEventToRelay(relayURL: string, event: ReturnType<typeof finalizeEvent>) {
  return Relay.connect(relayURL).then(async relay => {
    relay.publishTimeout = 5000

    try {
      await relay.publish(event)
    } finally {
      relay.close()
    }
  })
}

export async function publishSetupAnnouncement(
  currentAccount: Account,
  centralURL: string,
  secretKey: Uint8Array
) {
  const event = finalizeEvent(
    {
      kind: KIND_SETUP_ANNOUNCEMENT,
      created_at: Math.floor(Date.now() / 1000),
      tags: [
        [
          "m",
          bytesToHex(
            argon2id(utf8.encode(currentAccount.email), "pomegranate", { t: 1, m: 65536, p: 4 })
          )
        ],
        ["central", centralURL],
        ...currentAccount.operators.map(operator => ["operator", massageURL(operator.url)]),
        ["threshold", String(currentAccount.threshold)]
      ],
      content: ""
    },
    secretKey
  )

  await Promise.allSettled(
    SETUP_ANNOUNCEMENT_RELAYS.map(relayURL => publishEventToRelay(relayURL, event))
  )
}

export async function searchSetupAnnouncement(
  email: string
): Promise<{ centralURL: string; createdAt: number } | null> {
  if (email === "") {
    return null
  }

  try {
    const event = await pool.get(
      SETUP_ANNOUNCEMENT_RELAYS,
      {
        kinds: [KIND_SETUP_ANNOUNCEMENT],
        "#m": [bytesToHex(argon2id(utf8.encode(email), "pomegranate", { t: 1, m: 65536, p: 4 }))]
      },
      { maxWait: 5000 }
    )

    if (!event) return null

    const existingCentralURL = getFirstTagValue(event.tags, "central")
    if (!existingCentralURL) return null

    return {
      centralURL: massageURL(existingCentralURL),
      createdAt: event.created_at
    }
  } catch {
    return null
  } finally {
    pool.close(SETUP_ANNOUNCEMENT_RELAYS)
  }
}
