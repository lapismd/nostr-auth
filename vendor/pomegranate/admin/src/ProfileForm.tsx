import { Filter } from "@nostr/tools"
import { createSignal, Show } from "solid-js"
import type { ProfileFilter } from "./types"

export function ProfileForm(props: {
  creating: boolean
  onCancel: () => void
  onSubmit: (name: string, filter?: ProfileFilter) => Promise<void>
}) {
  const [name, setName] = createSignal("")
  const [useKinds, setUseKinds] = createSignal(false)
  const [kinds, setKinds] = createSignal("")
  const [useExpiration, setUseExpiration] = createSignal(false)
  const [expiration, setExpiration] = createSignal("")

  return (
    <form class="bg-white p-3 border rounded space-y-3" onSubmit={submit}>
      <input
        type="text"
        placeholder="Profile name"
        class="border p-2 w-full"
        value={name()}
        onInput={e => setName(e.target.value)}
      />

      <label class="flex items-center gap-2 text-sm">
        <input type="checkbox" checked={useKinds()} onChange={e => setUseKinds(e.target.checked)} />
        Limit event kinds
      </label>
      <Show when={useKinds()}>
        <input
          type="text"
          placeholder="1, 3, 7"
          class="border p-2 w-full"
          value={kinds()}
          onInput={e => setKinds(e.target.value)}
        />
      </Show>

      <label class="flex items-center gap-2 text-sm">
        <input
          type="checkbox"
          checked={useExpiration()}
          onChange={e => setUseExpiration(e.target.checked)}
        />
        Set expiration
      </label>
      <Show when={useExpiration()}>
        <input
          type="datetime-local"
          class="border p-2 w-full"
          value={expiration()}
          onInput={e => setExpiration(e.target.value)}
        />
      </Show>

      <div class="flex gap-2">
        <button
          type="submit"
          class="enabled:cursor-pointer bg-red-700 text-white p-2 flex-1 hover:bg-red-800 disabled:bg-gray-400"
          disabled={props.creating}
        >
          {props.creating ? "Creating profile..." : "Create profile"}
        </button>
        <button
          type="button"
          class="enabled:cursor-pointer border p-2 flex-1"
          disabled={props.creating}
          onClick={props.onCancel}
        >
          Cancel
        </button>
      </div>
    </form>
  )

  async function submit(event: SubmitEvent) {
    event.preventDefault()

    const trimmedName = name().trim()
    if (!trimmedName) {
      alert("name is required")
      return
    }

    const restrictions: Filter = {}

    if (useKinds()) {
      const parsedKinds = kinds()
        .split(",")
        .map(value => value.trim())
        .filter(value => value !== "")
        .map(value => Number(value))

      if (parsedKinds.length > 0 && parsedKinds.some(value => !Number.isInteger(value))) {
        alert("kinds must be comma-separated integers")
        return
      }

      if (parsedKinds.length > 0) {
        restrictions.kinds = parsedKinds
      }
    }

    if (useExpiration()) {
      if (!expiration()) {
        alert("expiration is required")
        return
      }

      const until = Math.floor(new Date(expiration()).getTime() / 1000)
      if (!Number.isFinite(until)) {
        alert("expiration is invalid")
        return
      }

      restrictions.until = until
    }

    await props.onSubmit(
      trimmedName,
      Object.keys(restrictions).length > 0 ? restrictions : undefined
    )
  }
}
