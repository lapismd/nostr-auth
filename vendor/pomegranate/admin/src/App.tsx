import { createEffect, createMemo, createSignal, For, Index, Show } from "solid-js"
import { generateSecretKey, finalizeEvent, getPublicKey } from "@nostr/tools"
import {
  hexPubShard,
  hexShard,
  trustedKeyDeal,
  aggregateSecretKeyShards,
  decodeShard
} from "@fiatjaf/promenade-trusted-dealer"
import { sha256 } from "@noble/hashes/sha2.js"
import { bytesToHex, hexToBytes } from "@noble/hashes/utils.js"
import { hexToNumber, numberToBytesBE } from "@noble/curves/utils.js"
import { decode, npubEncode, nsecEncode } from "@nostr/tools/nip19"
import type {
  Account,
  AccountOperator,
  ExistingSetupAnnouncement,
  Profile,
  ProfileFilter
} from "./types"
import { getTokenEmail, massageURL, normalizeOperators, formatTimestamp } from "./utils"
import { authenticate, clearStoredToken, getStoredAuth, storeAuth } from "./auth"
import {
  getAccount,
  fetchProfiles as apiFetchProfiles,
  createProfile as apiCreateProfile,
  deleteProfile as apiDeleteProfile,
  resetAccount as apiResetAccount
} from "./api"
import { publishSetupAnnouncement, searchSetupAnnouncement } from "./announcement"
import { ProfileForm } from "./ProfileForm"
import QRCode from "qrcode"

const utf8 = new TextEncoder()

const DEFAULT_OPERATORS = ["po.f7z.io", "po.coracle.social", "po.njump.me", "po.jumble.social"]

export default function App() {
  const [central, setCentral] = createSignal("auth.njump.me")
  const [operators, setOperators] = createSignal(
    [...DEFAULT_OPERATORS]
      .sort(() => Math.random() - 0.5)
      .slice(0, 3)
      .concat("")
  )
  const [steps, setSteps] = createSignal<{ label: string; done: boolean }[]>([])
  const [masterSk, setMasterSk] = createSignal<Uint8Array | null>(null)
  const [nsecInput, setNsecInput] = createSignal("")
  const [creatingProfile, setCreatingProfile] = createSignal(false)
  const [deletingProfile, setDeletingProfile] = createSignal("")
  const [profiles, setProfiles] = createSignal<Profile[]>([])
  const [loadingProfiles, setLoadingProfiles] = createSignal(false)
  const [profileRefresh, setProfileRefresh] = createSignal(0)
  const [showCreateProfileForm, setShowCreateProfileForm] = createSignal(false)
  const [qrPubkey, setQrPubkey] = createSignal("")
  const [loggingIn, setLoggingIn] = createSignal(false)
  const [resettingAccount, setResettingAccount] = createSignal(false)
  const [producingNsec, setProducingNsec] = createSignal(false)
  const [loggedInEmail, setLoggedInEmail] = createSignal("")
  const [account, setAccount] = createSignal<Account | null>(null)
  const [bootstrappedAccount, setBootstrappedAccount] = createSignal(false)
  const [recoveringOperatorURL, setRecoveringOperatorURL] = createSignal("")
  const [erasingOperatorURL, setErasingOperatorURL] = createSignal("")
  const [recoveredShards, setRecoveredShards] = createSignal<Record<string, string>>({})
  const [producedNsec, setProducedNsec] = createSignal("")
  const [existingSetupAnnouncement, setExistingSetupAnnouncement] =
    createSignal<ExistingSetupAnnouncement | null>(null)
  const [searchingExistingSetupAnnouncement, setSearchingExistingSetupAnnouncement] =
    createSignal(false)
  const [showManualRecovery, setShowManualRecovery] = createSignal(false)
  const [manualOperators, setManualOperators] = createSignal([""])
  const [manualShards, setManualShards] = createSignal<Record<string, string>>({})
  const [manualRecoveringURL, setManualRecoveringURL] = createSignal("")
  const [manualErasingURL, setManualErasingURL] = createSignal("")
  const [manualProducingNsec, setManualProducingNsec] = createSignal(false)
  const [manualProducedNsec, setManualProducedNsec] = createSignal("")
  const [findEmail, setFindEmail] = createSignal("")
  const [searchingCentral, setSearchingCentral] = createSignal(false)
  const [threshold, setThreshold] = createSignal(0)

  const recoveredShardCount = createMemo(() => Object.keys(recoveredShards()).length)
  const canProduceNsec = createMemo(
    () => recoveredShardCount() >= (account()?.threshold ?? Infinity)
  )
  const manualShardCount = createMemo(() => Object.keys(manualShards()).length)

  createEffect(() => {
    if (!account()) return

    profileRefresh()
    void fetchProfiles()
  })

  createEffect(() => {
    if (bootstrappedAccount()) return
    setBootstrappedAccount(true)

    const storedCentralURL = localStorage.getItem("centralURL")
    if (storedCentralURL) {
      setCentral(storedCentralURL)
    }

    void loadStoredAccount()
  })

  createEffect(() => {
    const count = operators().filter(op => op.trim() !== "").length
    if (count < 2) {
      setThreshold(2)
    } else {
      setThreshold(Math.ceil((count * 7) / 12))
    }
  })

  return (
    <div class="p-8 max-w-xl mx-auto">
      <Show when={showManualRecovery()}>
        <div class="border p-4 rounded space-y-3">
          <h2 class="text-lg font-bold">Manual Key Recovery</h2>
          <div class="text-sm text-gray-600">
            Enter operator URLs and recover shards from each. Once you have enough shards, you can
            produce the nsec.
          </div>
          <Index each={manualOperators()}>
            {(op, i) => (
              <div class="flex gap-2 items-start">
                <input
                  type="text"
                  placeholder="Operator URL"
                  class="border p-2 flex-1"
                  value={op()}
                  onInput={e => updateManualOperator(i, e.currentTarget.value)}
                />
                <button
                  class="enabled:cursor-pointer border p-2 hover:bg-gray-50 disabled:text-gray-400 disabled:border-gray-400 whitespace-nowrap"
                  disabled={manualRecoveringURL() === op() || op().trim() === ""}
                  onClick={() => void recoverManualShard(op())}
                >
                  {manualRecoveringURL() === op() ? "..." : "Recover"}
                </button>
                <button
                  class="enabled:cursor-pointer border p-2 text-red-700 hover:bg-red-50 disabled:text-gray-400 disabled:border-gray-400 whitespace-nowrap"
                  disabled={manualErasingURL() === op() || op().trim() === ""}
                  onClick={() => void eraseManualShard(op())}
                >
                  {manualErasingURL() === op() ? "Erasing..." : "Erase"}
                </button>
              </div>
            )}
          </Index>
          <Show when={manualShardCount() > 1}>
            <button
              class="enabled:cursor-pointer bg-red-700 text-white p-2 w-full hover:bg-red-800 disabled:bg-gray-400"
              disabled={manualProducingNsec()}
              onClick={() => void produceManualNsec()}
            >
              {manualProducingNsec() ? "Producing..." : "Produce nsec"}
            </button>
          </Show>
          <Show when={manualProducedNsec()}>
            <div class="border bg-white p-2 font-mono text-sm break-all">
              {manualProducedNsec()}
            </div>
          </Show>
          <Show when={manualShardCount() > 0}>
            <div class="text-sm text-gray-700">Recovered shards: {manualShardCount()}</div>
          </Show>
          <button
            class="enabled:cursor-pointer border p-2 w-full hover:bg-gray-50"
            onClick={exitManualRecovery}
          >
            Back
          </button>
        </div>
      </Show>
      <Show when={!showManualRecovery()}>
        <h1 class="text-xl font-bold mb-4">Pomegranate Admin (or demo)</h1>
        <div class="mb-4">
          <a
            href="http://viewsource.win/npub180cvv07tjdrrgpa0j7j7tmnyl2yr6yr7l8j4s3evf6u64th6gkwsyjh6w6/pomegranate"
            class="text-sm text-gray-600 underline hover:text-gray-800"
            target="_blank"
          >
            More information about Pomegranate
          </a>
        </div>
        <Show
          when={loggedInEmail()}
          fallback={
            <form
              onSubmit={ev => {
                ev.preventDefault()
                login()
              }}
            >
              <input
                type="text"
                placeholder="Central URL"
                class="border p-2 w-full mb-2"
                value={central()}
                onInput={e => setCentral(e.currentTarget.value)}
              />

              <button
                class="enabled:cursor-pointer bg-red-700 text-white p-2 w-full hover:bg-red-800 disabled:bg-gray-400"
                disabled={loggingIn() || central().trim() === ""}
              >
                {loggingIn() ? "Logging in..." : "Login"}
              </button>
            </form>
          }
        >
          <div class="mt-4 flex items-center justify-between">
            <span class="text-sm text-gray-700">Logged in as {loggedInEmail()}</span>
            <button
              class="enabled:cursor-pointer border border-red-700 text-red-700 px-3 py-1 hover:bg-red-50"
              onClick={logout}
            >
              Logout
            </button>
          </div>
        </Show>

        <Show when={!account() && loggedInEmail()}>
          <div class="mt-4">
            <Show when={searchingExistingSetupAnnouncement()}>
              <div class="mb-3 text-sm text-gray-600">Looking for existing account...</div>
            </Show>
            <Show when={existingSetupAnnouncement()}>
              <div class="mb-3 border border-amber-300 bg-amber-50 p-3 text-sm text-amber-900 space-y-2">
                <div>
                  You already have account on server {existingSetupAnnouncement()!.centralURL}{" "}
                  created at {formatTimestamp(existingSetupAnnouncement()!.createdAt)}
                </div>
                <button
                  class="enabled:cursor-pointer border border-amber-700 px-3 py-2 hover:bg-amber-100 disabled:border-gray-400 disabled:text-gray-400"
                  disabled={loggingIn()}
                  onClick={() => void login(existingSetupAnnouncement()!.centralURL)}
                >
                  {loggingIn() ? "Connecting..." : "Connect to existing account"}
                </button>
              </div>
            </Show>
            <Show
              when={masterSk()}
              fallback={
                <div class="space-y-2">
                  <input
                    type="text"
                    placeholder="nsec..."
                    class="border p-2 w-full"
                    value={nsecInput()}
                    onInput={e => setNsecInput(e.currentTarget.value)}
                  />
                  <div class="flex gap-2">
                    <button
                      class="enabled:cursor-pointer border p-2 w-full disabled:text-gray-400 disabled:border-gray-400"
                      disabled={nsecInput().trim() === ""}
                      onClick={importSecretKey}
                    >
                      Use nsec
                    </button>
                    <button
                      class="enabled:cursor-pointer bg-red-700 text-white p-2 w-full hover:bg-red-800"
                      onClick={generateNewSecretKey}
                    >
                      Generate new nsec
                    </button>
                  </div>
                </div>
              }
            >
              <div class="mb-3 text-sm text-gray-700 font-mono">
                Secret Key:{" "}
                <span class="truncate max-w-[200px] align-bottom inline-block">
                  {nsecEncode(masterSk()!)}
                </span>
              </div>
              <Index each={operators()}>
                {(op, i) => (
                  <input
                    type="text"
                    placeholder="Operator URL"
                    class="border p-2 w-full mb-1"
                    value={op()}
                    onInput={e => updateOperator(i, e.currentTarget.value)}
                  />
                )}
              </Index>
              <div class="flex items-center gap-2 text-sm my-1">
                <label>Threshold:</label>
                <input
                  type="number"
                  min={2}
                  max={Math.max(2, operators().filter(op => op.trim() !== "").length)}
                  value={threshold()}
                  onInput={e => {
                    const val = parseInt(e.currentTarget.value)
                    if (!Number.isNaN(val)) setThreshold(val)
                  }}
                  class="border p-2 w-20"
                />
                <span class="text-gray-500">
                  / {operators().filter(op => op.trim() !== "").length} operators
                </span>
              </div>
            </Show>
          </div>

          <Show when={masterSk()}>
            <button
              class="enabled:cursor-pointer bg-red-700 text-white p-2 w-full hover:bg-red-800 disabled:bg-gray-400"
              disabled={loggingIn()}
              onClick={create}
            >
              Create
            </button>

            <ul class="mt-4">
              <For each={steps()}>
                {step => (
                  <li class={step.done ? "text-green-600" : "text-gray-400"}>
                    {step.done ? "\u2713" : "\u25CB"} {step.label}
                  </li>
                )}
              </For>
            </ul>
          </Show>
        </Show>

        <Show when={account()}>
          <div
            class="mt-4 p-4 space-y-3"
            style={{
              "background-color": `hsl(${hexToNumber(account()!.pubkey.slice(22, 26)) % 360n} 50 85)`
            }}
          >
            <div>Account ready</div>
            <div class="border bg-white p-2 font-mono text-sm break-all" title={account()!.pubkey}>
              {npubEncode(account()!.pubkey)}
            </div>
            <div class="flex gap-2">
              <button
                class="enabled:cursor-pointer border border-red-700 text-red-700 p-2 w-full hover:bg-red-50 disabled:border-gray-400 disabled:text-gray-400"
                disabled={resettingAccount()}
                onClick={resetAccount}
              >
                {resettingAccount() ? "Resetting account..." : "Reset account"}
              </button>
              <button
                class="enabled:cursor-pointer bg-red-700 text-white p-2 w-full hover:bg-red-800 disabled:bg-gray-400"
                disabled={producingNsec() || !canProduceNsec() || producedNsec() !== ""}
                onClick={produceNsec}
                title={
                  canProduceNsec()
                    ? "Recover enough shards from the operators first so the nsec can be produced"
                    : ""
                }
              >
                {producingNsec() ? "Producing nsec..." : "Produce nsec"}
              </button>
            </div>
            <Show when={account()}>
              <div class="text-sm text-gray-700">
                Recovered shards: {recoveredShardCount()} / {account()!.threshold}
              </div>
            </Show>
            <Show when={producedNsec()}>
              <div class="border bg-white p-2 font-mono text-sm break-all">{producedNsec()}</div>
            </Show>
            <table class="w-full border-collapse bg-white text-sm">
              <thead>
                <tr>
                  <th class="border p-2 text-left">Operator URL</th>
                  <th class="border p-2 text-left">Public Shard</th>
                  <th class="border p-2 text-left">Actions</th>
                </tr>
              </thead>
              <tbody>
                <For each={account()?.operators ?? []}>
                  {operator => (
                    <tr>
                      <td class="border p-2 break-all">{operator.url}</td>
                      <td class="border p-2 break-all font-mono">{operator.pubshard}</td>
                      <td class="border p-2">
                        <div class="flex gap-2">
                          <button
                            class="enabled:cursor-pointer border p-2 w-full hover:bg-gray-50 disabled:text-gray-400 disabled:border-gray-400 whitespace-nowrap flex-1"
                            disabled={recoveringOperatorURL() === operator.url}
                            onClick={() => void recoverShard(operator)}
                          >
                            {recoveringOperatorURL() === operator.url ? "Recovering..." : "Recover"}
                          </button>
                          <button
                            class="enabled:cursor-pointer border p-2 w-full hover:bg-red-50 text-red-700 disabled:text-gray-400 disabled:border-gray-400 whitespace-nowrap flex-1"
                            disabled={erasingOperatorURL() === operator.url}
                            onClick={() => void eraseShard(operator)}
                          >
                            {erasingOperatorURL() === operator.url ? "Erasing..." : "Erase"}
                          </button>
                        </div>
                      </td>
                    </tr>
                  )}
                </For>
              </tbody>
            </table>
            <Show when={loadingProfiles()}>
              <div class="text-gray-600">Loading profiles...</div>
            </Show>
            <Show when={profiles().length > 0}>
              <button
                class="enabled:cursor-pointer bg-red-700 text-white p-2 w-full hover:bg-red-800"
                onClick={() => setShowCreateProfileForm(true)}
              >
                Create new signing profile
              </button>
              <Show when={showCreateProfileForm()}>
                <ProfileForm
                  creating={creatingProfile()}
                  onCancel={() => setShowCreateProfileForm(false)}
                  onSubmit={createProfile}
                />
              </Show>
              <ul class="space-y-1">
                <For each={profiles()}>
                  {profile => (
                    <li class="bg-white p-2 border rounded">
                      <div class="flex items-start justify-between gap-2">
                        <div class="font-medium">{profile.name}</div>
                        <button
                          class="enabled:cursor-pointer border border-red-700 text-red-700 hover:bg-red-50 px-2 py-1 rounded disabled:border-gray-400 disabled:text-gray-400 mb-4"
                          disabled={deletingProfile() === profile.handler_pubkey}
                          onClick={() => void deleteProfile(profile)}
                        >
                          {deletingProfile() === profile.handler_pubkey ? "Deleting..." : "Delete"}
                        </button>
                      </div>
                      <div class="text-sm text-gray-600 break-all font-mono">
                        bunker://{profile.handler_pubkey}?relay=
                        {encodeURIComponent(central().replace("http", "ws"))}
                      </div>
                      <button
                        class="enabled:cursor-pointer border px-2 py-1 rounded hover:bg-gray-50 whitespace-nowrap"
                        onClick={() =>
                          setQrPubkey(
                            qrPubkey() === profile.handler_pubkey ? "" : profile.handler_pubkey
                          )
                        }
                      >
                        {qrPubkey() === profile.handler_pubkey ? "Hide QR" : "Show QR"}
                      </button>
                      <Show when={qrPubkey() === profile.handler_pubkey}>
                        <div class="flex gap-4 mt-2">
                          <QrImage
                            value={
                              "bunker://" +
                              profile.handler_pubkey +
                              "?relay=" +
                              encodeURIComponent(central().replace("http", "ws"))
                            }
                            label={`bunker://${profile.handler_pubkey.slice(0, 8)}…`}
                          />
                        </div>
                      </Show>
                    </li>
                  )}
                </For>
              </ul>
            </Show>
            <Show when={profiles().length === 0}>
              <button
                class="enabled:cursor-pointer bg-red-700 text-white p-2 w-full hover:bg-red-800 disabled:bg-gray-400"
                disabled={creatingProfile()}
                onClick={createDefaultProfile}
              >
                {creatingProfile() ? "Creating profile..." : "Create default signing profile"}
              </button>
            </Show>
          </div>
        </Show>

        <Show when={!loggedInEmail()}>
          <div class="mt-16 border-t pt-12 space-y-3">
            <div class="text-sm text-gray-600 mb-2">
              Already registered, but don't know about Central servers? Enter your email address to
              find yours.
            </div>
            <form
              class="flex gap-2"
              onSubmit={ev => {
                ev.preventDefault()
                findYourCentral()
              }}
            >
              <input
                type="email"
                placeholder="email@example.com"
                class="border p-2 flex-1"
                value={findEmail()}
                onInput={e => setFindEmail(e.currentTarget.value)}
              />
              <button
                class="enabled:cursor-pointer bg-red-700 text-white p-2 hover:bg-red-800 disabled:bg-gray-400 whitespace-nowrap"
                disabled={searchingCentral() || findEmail().trim() === ""}
              >
                {searchingCentral() ? "Searching..." : "Find your central"}
              </button>
            </form>
          </div>
          <div class="mt-16 border-t pt-12 space-y-3">
            <div class="text-sm text-gray-600 mb-2">
              Your Central server doesn't exist anymore? Talk direct to the Operators to recover
              your key:
            </div>
            <button
              class="enabled:cursor-pointer border p-2 w-full hover:bg-gray-50"
              onClick={enterManualRecovery}
            >
              Start
            </button>
          </div>
        </Show>
      </Show>
    </div>
  )

  function updateOperator(index: number, value: string) {
    const newOperators = [...operators()]
    newOperators[index] = value

    setOperators(normalizeOperators(newOperators))
  }

  function updateManualOperator(index: number, value: string) {
    const newOperators = [...manualOperators()]
    newOperators[index] = value

    setManualOperators(normalizeOperators(newOperators))
  }

  function enterManualRecovery() {
    setShowManualRecovery(true)
    setManualOperators([""])
    setManualShards({})
    setManualRecoveringURL("")
    setManualErasingURL("")
    setManualProducingNsec(false)
    setManualProducedNsec("")
  }

  function exitManualRecovery() {
    setShowManualRecovery(false)
    setManualOperators([""])
    setManualShards({})
    setManualRecoveringURL("")
    setManualErasingURL("")
    setManualProducingNsec(false)
    setManualProducedNsec("")
  }

  async function recoverManualShard(operatorURL: string) {
    const url = massageURL(operatorURL)
    const popup = window.open(`${url}/po/recover/google`, "Recover", "width=600,height=600")
    if (!popup) {
      alert("failed to open recovery popup")
      return
    }

    setManualRecoveringURL(operatorURL)

    try {
      const shard = await new Promise<string>((resolve, reject) => {
        const popupMonitor = window.setInterval(() => {
          if (!popup.closed) return
          cleanup()
          reject(null)
        }, 250)

        window.addEventListener("message", handler)

        function cleanup() {
          window.removeEventListener("message", handler)
          window.clearInterval(popupMonitor)
        }

        function handler(event: MessageEvent) {
          if (event.origin !== url || event.source !== popup || typeof event.data !== "string")
            return
          cleanup()
          popup!.close()
          resolve(event.data)
        }
      })

      setManualShards(current => ({ ...current, [operatorURL]: shard }))
    } catch (error) {
      if (error !== null) {
        alert(error instanceof Error ? error.message : "recovery failed")
      }
    } finally {
      setManualRecoveringURL("")
    }
  }

  async function eraseManualShard(operatorURL: string) {
    if (!confirm(`Erase your data from operator ${operatorURL}?`)) {
      return
    }

    const url = massageURL(operatorURL)
    const popup = window.open(`${url}/po/erase/google`, "Erase", "width=600,height=600")
    if (!popup) {
      alert("failed to open erase popup")
      return
    }

    setManualErasingURL(operatorURL)

    try {
      await new Promise<void>((resolve, reject) => {
        const popupMonitor = window.setInterval(() => {
          if (!popup.closed) return
          cleanup()
          reject(null)
        }, 250)

        window.addEventListener("message", handler)

        function cleanup() {
          window.removeEventListener("message", handler)
          window.clearInterval(popupMonitor)
        }

        function handler(event: MessageEvent) {
          if (event.origin !== url || event.source !== popup || typeof event.data !== "string")
            return
          cleanup()
          popup!.close()
          resolve()
        }
      })

      setManualShards(current => {
        const next = { ...current }
        delete next[operatorURL]
        return next
      })
    } catch (error) {
      if (error !== null) {
        alert(error instanceof Error ? error.message : "erase failed")
      }
    } finally {
      setManualErasingURL("")
    }
  }

  async function produceManualNsec() {
    const shards = Object.values(manualShards())
    setManualProducingNsec(true)
    try {
      const secretKey = numberToBytesBE(
        aggregateSecretKeyShards(shards.map(hexToBytes).map(decodeShard)),
        32
      )
      setManualProducedNsec(nsecEncode(secretKey))
    } catch (error) {
      alert(error instanceof Error ? error.message : "Failed to produce nsec")
    } finally {
      setManualProducingNsec(false)
    }
  }

  async function loadStoredAccount() {
    const auth = getStoredAuth()
    if (!auth) return

    setCentral(auth.centralURL)
    const email = getTokenEmail(auth.token)
    setLoggedInEmail(email)
    searchForExistingSetupAnnouncement(email, auth.centralURL)

    await refreshAccount(auth)
  }

  async function refreshAccount(auth: { token: string; centralURL: string }) {
    setAccount(null)
    setRecoveredShards({})
    setRecoveringOperatorURL("")
    setErasingOperatorURL("")
    setProducedNsec("")

    const loadedAccount = await getAccount(auth.token, auth.centralURL)
    if (loadedAccount === null) {
      clearStoredToken()
      return
    }

    setOperators(normalizeOperators(loadedAccount.operators.map(operator => operator.url)))
    setLoggedInEmail(loadedAccount.email)
    setAccount(loadedAccount)
    setCreatingProfile(false)
    setShowCreateProfileForm(false)
    setSteps(s => s.map(step => ({ ...step, done: true })))
  }

  async function login(nextCentralURL?: string) {
    setLoggingIn(true)

    try {
      const centralURL = massageURL(nextCentralURL ?? central())
      setCentral(centralURL)

      const token = await authenticate(centralURL)
      const email = getTokenEmail(token)
      setLoggedInEmail(email)
      setProfiles([])
      setLoadingProfiles(false)
      setProfileRefresh(0)
      setShowCreateProfileForm(false)
      setRecoveredShards({})
      setRecoveringOperatorURL("")
      setErasingOperatorURL("")
      setProducedNsec("")
      setSteps(s => s.map(step => ({ ...step, done: false })))
      searchForExistingSetupAnnouncement(email, centralURL)
      await refreshAccount({ token, centralURL })
    } finally {
      setLoggingIn(false)
    }
  }

  function logout() {
    clearStoredToken()
    setLoggedInEmail("")
    setAccount(null)
    setProfiles([])
    setMasterSk(null)
    setRecoveredShards({})
    setRecoveringOperatorURL("")
    setErasingOperatorURL("")
    setProducedNsec("")
    setExistingSetupAnnouncement(null)
    setSteps(s => s.map(step => ({ ...step, done: false })))
  }

  async function create() {
    const secretKey = masterSk()
    if (!secretKey) {
      alert("missing secret key")
      return
    }

    setCreatingProfile(false)
    setProfiles([])
    setLoadingProfiles(false)
    setProfileRefresh(0)
    setShowCreateProfileForm(false)
    const ops = operators()
      .filter(op => op.trim() !== "")
      .map(op => massageURL(op))
    if (ops.length < 2) {
      alert("need at least 2 operators")
      return
    }

    setSteps([
      { label: "Preparing key", done: false },
      { label: "Splitting secret", done: false },
      { label: "Registering with central", done: false },
      ...ops.map((_, i) => ({ label: `Registering with operator ${i + 1}`, done: false }))
    ])

    const thresholdValue = threshold()
    if (thresholdValue < 2 || thresholdValue > ops.length) {
      alert("invalid threshold")
      return
    }
    const centralURL = massageURL(central())
    const session = crypto.randomUUID()

    const auth = getStoredAuth()
    let token = auth?.token ?? null
    if (!token) {
      token = await authenticate(centralURL)
    } else if (auth?.centralURL !== centralURL) {
      storeAuth(token, centralURL)
    }

    // 1. Generate keys
    setSteps(s => s.map((step, i) => (i === 0 ? { ...step, done: true } : step)))

    // 2. Split secret
    const masterSkBignum = Array.from(secretKey).reduce(
      (acc, byte) => (acc << 8n) + BigInt(byte),
      0n
    )
    const { shards } = trustedKeyDeal(masterSkBignum, thresholdValue, ops.length)
    setSteps(s => s.map((step, i) => (i === 1 ? { ...step, done: true } : step)))

    // 3. Register with central
    const regEvent = finalizeEvent(
      {
        kind: 20445,
        created_at: Math.floor(Date.now() / 1000),
        tags: [
          ["threshold", String(thresholdValue)],
          ...ops.map((op, i) => ["operator", op, hexPubShard(shards[i].pubShard)])
        ],
        content: ""
      },
      secretKey
    )

    const regResp = await fetch(centralURL + "/register", {
      method: "POST",
      body: JSON.stringify(regEvent),
      headers: {
        "Content-Type": "application/json",
        Authorization: "Token " + token,
        "X-Pomegranate-Session": session
      }
    })

    if (regResp.status === 200) {
      setSteps(s => s.map((step, i) => (i === 2 ? { ...step, done: true } : step)))
    } else {
      alert("central registration failed")
      return
    }

    // 4. Register with operators
    for (let i = 0; i < ops.length; i++) {
      const shard = shards[i]
      const event = finalizeEvent(
        {
          kind: 20444,
          created_at: Math.floor(Date.now() / 1000),
          tags: [
            ["central", centralURL],
            ["email", loggedInEmail()]
          ],
          content: hexShard(shard)
        },
        secretKey
      )

      const opResp = await fetch(ops[i] + "/po/register", {
        method: "POST",
        body: JSON.stringify(event),
        headers: {
          "Content-Type": "application/json",
          "X-Pomegranate-Operator-Token": bytesToHex(sha256(utf8.encode(session + ":" + ops[i])))
        }
      })
      if (!opResp.ok) {
        alert(`operator registration failed for ${ops[i]}`)
        return
      }

      setSteps(s => s.map((step, j) => (j === 3 + i ? { ...step, done: true } : step)))
    }

    const storedAuth = getStoredAuth()
    if (storedAuth) {
      await refreshAccount(storedAuth)

      const loadedAccount = account()
      if (loadedAccount && getPublicKey(secretKey) === loadedAccount.pubkey) {
        publishSetupAnnouncement(loadedAccount, centralURL, secretKey)
      }
    }
  }

  async function searchForExistingSetupAnnouncement(email: string, currentCentralURL: string) {
    if (email === "") {
      setExistingSetupAnnouncement(null)
      setSearchingExistingSetupAnnouncement(false)
      return
    }

    setExistingSetupAnnouncement(null)
    setSearchingExistingSetupAnnouncement(true)

    try {
      const result = await searchSetupAnnouncement(email)
      if (result && result.centralURL !== currentCentralURL) {
        setExistingSetupAnnouncement(result)
      }
    } catch (err) {
      console.warn("search for existing setup announcement failed", err)
      setExistingSetupAnnouncement(null)
    } finally {
      setSearchingExistingSetupAnnouncement(false)
    }
  }

  function importSecretKey() {
    try {
      const decoded = decode(nsecInput().trim())
      if (decoded.type !== "nsec" || !(decoded.data instanceof Uint8Array)) {
        throw new Error("invalid nsec")
      }

      setMasterSk(decoded.data)
      setNsecInput("")
    } catch (err) {
      console.warn("invalid nsec", err)
      alert("invalid nsec: " + String(err))
    }
  }

  function generateNewSecretKey() {
    setMasterSk(generateSecretKey())
    setNsecInput("")
  }

  async function createProfile(name: string, restrictions?: ProfileFilter) {
    const auth = getStoredAuth()
    if (!auth) {
      alert("missing auth token")
      return
    }

    setCreatingProfile(true)

    try {
      await apiCreateProfile(auth.token, auth.centralURL, name, restrictions)
      setShowCreateProfileForm(false)
      setProfileRefresh(count => count + 1)
    } catch (err) {
      console.warn("profile creation failed", err)
      alert("profile creation failed: " + String(err))
    } finally {
      setCreatingProfile(false)
    }
  }

  function createDefaultProfile() {
    void createProfile("default")
  }

  async function fetchProfiles() {
    const auth = getStoredAuth()
    if (!auth) {
      setProfiles([])
      return
    }

    setLoadingProfiles(true)

    try {
      setProfiles(await apiFetchProfiles(auth.token, auth.centralURL))
    } catch (err) {
      console.warn("failed to load profiles", err)
      alert("failed to load profiles: " + String(err))
    } finally {
      setLoadingProfiles(false)
    }
  }

  async function deleteProfile(profile: Profile) {
    if (!confirm(`Delete profile '${profile.name}'?`)) {
      return
    }

    const auth = getStoredAuth()
    if (!auth) {
      alert("missing auth token")
      return
    }

    setDeletingProfile(profile.handler_pubkey)

    try {
      await apiDeleteProfile(auth.token, auth.centralURL, profile.handler_pubkey)
      setProfileRefresh(count => count + 1)
    } catch (err) {
      console.warn("profile deletion failed", err)
      alert("profile deletion failed: " + String(err))
    } finally {
      setDeletingProfile("")
    }
  }

  async function resetAccount() {
    if (!confirm("Reset account and delete all profiles?")) {
      return
    }

    const auth = getStoredAuth()
    if (!auth) {
      alert("missing auth token")
      return
    }

    setResettingAccount(true)

    try {
      await apiResetAccount(auth.token, auth.centralURL)
      setAccount(null)
      setProfiles([])
      setLoadingProfiles(false)
      setProfileRefresh(0)
      setShowCreateProfileForm(false)
      setCreatingProfile(false)
      setDeletingProfile("")
      setRecoveredShards({})
      setRecoveringOperatorURL("")
      setErasingOperatorURL("")
      setProducedNsec("")
      setSteps(s => s.map(step => ({ ...step, done: false })))
    } catch (err) {
      console.warn("account reset failed", err)
      alert("account reset failed: " + String(err))
    } finally {
      setResettingAccount(false)
    }
  }

  async function recoverShard(operator: AccountOperator) {
    const operatorURL = massageURL(operator.url)
    const popup = window.open(`${operatorURL}/po/recover/google`, "Recover", "width=600,height=600")
    if (!popup) {
      alert("failed to open recovery popup")
      return
    }

    setRecoveringOperatorURL(operator.url)

    try {
      const shard = await new Promise<string>((resolve, reject) => {
        const popupMonitor = window.setInterval(() => {
          if (!popup.closed) {
            return
          }

          cleanup()
          reject(null)
        }, 250)

        window.addEventListener("message", handler)

        function cleanup() {
          window.removeEventListener("message", handler)
          window.clearInterval(popupMonitor)
        }

        function handler(event: MessageEvent) {
          if (
            event.origin !== operatorURL ||
            event.source !== popup ||
            typeof event.data !== "string"
          ) {
            return
          }

          cleanup()
          popup!.close()
          resolve(event.data)
        }
      })

      if (!shard.startsWith(operator.pubshard)) {
        throw new Error("Recovered shard does not match operator")
      }

      setRecoveredShards(current => ({ ...current, [operator.url]: shard }))
      setProducedNsec("")
    } catch (error) {
      if (error !== null) {
        alert(error instanceof Error ? error.message : "recovery failed")
      }
    } finally {
      setRecoveringOperatorURL("")
    }
  }

  async function eraseShard(operator: AccountOperator) {
    if (!confirm(`Erase your data from operator ${operator.url}?`)) {
      return
    }

    const url = massageURL(operator.url)
    const popup = window.open(`${url}/po/erase/google`, "Erase", "width=600,height=600")
    if (!popup) {
      alert("failed to open erase popup")
      return
    }

    setErasingOperatorURL(operator.url)

    try {
      await new Promise<void>((resolve, reject) => {
        const popupMonitor = window.setInterval(() => {
          if (!popup.closed) return
          cleanup()
          reject(null)
        }, 250)

        window.addEventListener("message", handler)

        function cleanup() {
          window.removeEventListener("message", handler)
          window.clearInterval(popupMonitor)
        }

        function handler(event: MessageEvent) {
          if (event.origin !== url || event.source !== popup || typeof event.data !== "string")
            return
          cleanup()
          popup!.close()
          resolve()
        }
      })

      setRecoveredShards(current => {
        const next = { ...current }
        delete next[operator.url]
        return next
      })
      setProducedNsec("")
    } catch (error) {
      if (error !== null) {
        alert(error instanceof Error ? error.message : "erase failed")
      }
    } finally {
      setErasingOperatorURL("")
    }
  }

  async function produceNsec() {
    const currentAccount = account()
    if (!currentAccount) {
      return
    }

    const shards = currentAccount.operators
      .map(operator => recoveredShards()[operator.url])
      .filter((shard): shard is string => Boolean(shard))
      .slice(0, currentAccount.threshold)

    if (shards.length < currentAccount.threshold) {
      alert("need more recovered shards")
      return
    }

    setProducingNsec(true)

    try {
      const secretKey = numberToBytesBE(
        aggregateSecretKeyShards(shards.map(hexToBytes).map(decodeShard)),
        32
      )
      const pubkey = getPublicKey(secretKey)
      if (pubkey !== currentAccount.pubkey) {
        throw new Error("Recovered shards produced wrong key")
      }

      setMasterSk(secretKey)
      setProducedNsec(nsecEncode(secretKey))
    } catch (error) {
      alert(error instanceof Error ? error.message : "Failed to produce nsec")
    } finally {
      setProducingNsec(false)
    }
  }

  async function findYourCentral() {
    const email = findEmail().trim()
    if (!email) return

    setSearchingCentral(true)

    try {
      const result = await searchSetupAnnouncement(email)
      if (result) {
        setCentral(result.centralURL)
        setFindEmail("")
      } else {
        alert("no central found for this email")
      }
    } catch (err) {
      alert("search failed: " + String(err))
    } finally {
      setSearchingCentral(false)
    }
  }
}

function QrImage(props: { value: string; label: string }) {
  const [src, setSrc] = createSignal("")

  createEffect(() => {
    void QRCode.toDataURL(props.value, { margin: 1, width: 256 }).then(setSrc)
  })

  return (
    <div class="flex flex-col items-center gap-1">
      <Show when={src() !== ""}>
        <img src={src()} alt={props.label} class="w-48 h-48" />
      </Show>
      <div class="text-xs text-gray-500 break-all text-center">{props.label}</div>
    </div>
  )
}
