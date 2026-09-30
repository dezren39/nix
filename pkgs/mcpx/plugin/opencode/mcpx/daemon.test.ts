/**
 * The discovery ladder, against real sockets.
 *
 * Every test here builds a temporary state directory and serves fake daemons
 * on real unix sockets with `Bun.serve({unix})`, because the failures this
 * ladder exists to survive are all about the filesystem and the socket: a
 * stale info file, a socket with nothing behind it, two daemons at once. A
 * mocked fetch would pass every one of them.
 *
 *   bun test plugin/opencode/mcpx/daemon.test.ts
 */

import { afterEach, describe, expect, test } from "bun:test"
import { mkdtempSync, mkdirSync, writeFileSync, rmSync, chmodSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"

import {
  discover,
  goDurationMs,
  liveCandidates,
  rank,
  readInfoFiles,
  splitArgs,
  targetKey,
  writeRemembered,
  readRemembered,
  type Candidate,
  type DaemonInfo,
} from "./daemon.ts"

// Short by design. A unix socket path is capped at 104 bytes on macOS, and
// the daemon itself moves its socket elsewhere when the state path would
// overflow -- a test directory under a long temp path would be testing the
// wrong thing.
const scratch: string[] = []
const tempRoot = (): string => {
  const dir = mkdtempSync(join(tmpdir(), "mcpxt-"))
  scratch.push(dir)
  return dir
}

/**
 * An environment with no ambient daemons in it.
 *
 * TMPDIR matters as much as the state directory: the daemon falls back to a
 * private per-user directory under the temp directory when the state path is
 * too long for a unix socket, so discovery looks there -- and on a developer
 * machine that directory holds their real daemons. Without this the suite
 * passes or fails depending on what happens to be running.
 */
const envFor = (state: string): Record<string, string | undefined> => ({
  MCPX_STATE_DIR: state,
  TMPDIR: tempRoot(),
})

type Fake = {
  socket: string
  stop: () => void
  /** Requests it received, so a test can prove a rung was or was not used. */
  seen: string[]
}

type FakeOptions = {
  /** What /v1/resolve answers. Omitted means the route is absent (404). */
  resolve?: (dir: string) => unknown
  config?: string
  version?: string
  servers?: number
}

const serveFake = (socket: string, opts: FakeOptions = {}): Fake => {
  const seen: string[] = []
  const server = Bun.serve({
    unix: socket,
    fetch(req) {
      const url = new URL(req.url)
      seen.push(url.pathname)
      if (url.pathname === "/v1/health") {
        return Response.json({ status: "ok", version: opts.version ?? "test", pid: 1 })
      }
      if (url.pathname === "/v1/status") {
        return Response.json({
          version: opts.version ?? "test",
          pid: 1,
          socket,
          config: opts.config,
          servers: Array.from({ length: opts.servers ?? 0 }, (_, i) => ({
            name: `s${i}`,
            live: 1,
            instances: [{ key: `ses-${i}` }],
          })),
        })
      }
      if (url.pathname === "/v1/resolve") {
        if (!opts.resolve) return new Response("not found", { status: 404 })
        return Response.json(opts.resolve(url.searchParams.get("dir") ?? ""))
      }
      return new Response("not found", { status: 404 })
    },
  })
  return { socket, seen, stop: () => server.stop(true) }
}

const stopAll: Array<() => void> = []
const fake = (socket: string, opts?: FakeOptions): Fake => {
  const f = serveFake(socket, opts)
  stopAll.push(f.stop)
  return f
}

const writeInfo = (state: string, key: string, info: DaemonInfo): string => {
  const path = join(state, `daemon-${key}.json`)
  writeFileSync(path, JSON.stringify(info, null, 2))
  return path
}

afterEach(() => {
  while (stopAll.length) stopAll.pop()!()
  while (scratch.length) rmSync(scratch.pop()!, { recursive: true, force: true })
})

describe("rung 2: the info files", () => {
  test("no binary and one daemon: everything works", async () => {
    const state = tempRoot()
    const socket = join(state, "d-one.sock")
    fake(socket, { config: "/proj/.mcpx.json", servers: 3 })
    writeInfo(state, "aaa", {
      socket,
      configPath: "/proj/.mcpx.json",
      startedAt: new Date().toISOString(),
      version: "0.1.0",
    })

    // bin: "" is "there is no mcpx on PATH". Nothing below rung 2 may run.
    const got = await discover({ env: envFor(state), bin: "", directory: "/proj" })
    expect(got.reason).toBe("only")
    expect(got.rung).toBe(2)
    expect(got.ambiguous).toBe(false)
    expect(targetKey(got.target!)).toBe("unix://" + socket)
    expect(got.chosen?.servers).toBe(3)
    expect(got.chosen?.sessions).toBe(3)
  })

  test("a stale info file is not a candidate", async () => {
    const state = tempRoot()
    const live = join(state, "d-live.sock")
    fake(live, { config: "/proj/.mcpx.json" })
    writeInfo(state, "live", { socket: live })
    // A daemon that crashed: the file outlives the process, and the socket
    // file may or may not still be there. Neither is a daemon.
    writeInfo(state, "dead", { socket: join(state, "d-dead.sock") })
    writeFileSync(join(state, "d-dead.sock"), "")

    expect(readInfoFiles({ MCPX_STATE_DIR: state })).toHaveLength(2)
    const candidates = await liveCandidates({ env: envFor(state) })
    expect(candidates).toHaveLength(1)
    expect(candidates[0].target).toEqual({ kind: "socket", path: live })

    const got = await discover({ env: envFor(state), bin: "" })
    expect(got.reason).toBe("only")
  })

  test("a socket with no info file is still a daemon", async () => {
    const state = tempRoot()
    const socket = join(state, "daemon-orphan.sock")
    fake(socket, { config: "/proj/.mcpx.json" })

    const got = await discover({ env: envFor(state), bin: "" })
    expect(got.reason).toBe("only")
    expect(got.chosen?.source).toBe("socket")
  })

  test("a socket in the private runtime directory is found", async () => {
    const state = tempRoot()
    const runtime = tempRoot()
    mkdirSync(join(runtime, "mcpx"), { recursive: true })
    const socket = join(runtime, "mcpx", "d-x.sock")
    fake(socket, { config: "/proj/.mcpx.json" })

    const got = await discover({
      env: { ...envFor(state), XDG_RUNTIME_DIR: runtime },
      bin: "",
    })
    expect(targetKey(got.target!)).toBe("unix://" + socket)
  })

  test("no daemon at all is a fact, not a failure", async () => {
    const state = tempRoot()
    const got = await discover({ env: envFor(state), bin: "" })
    expect(got.reason).toBe("none")
    expect(got.target).toBeUndefined()
    expect(got.candidates).toEqual([])
  })
})

describe("rung 3: two daemons", () => {
  test("no binary and two daemons: one of them resolves the directory", async () => {
    const state = tempRoot()
    const mine = join(state, "d-mine.sock")
    const other = join(state, "d-other.sock")
    const answer = (dir: string) => ({
      socket: dir.startsWith("/proj") ? mine : other,
      endpoint: "",
      configPath: "/proj/.mcpx.json",
      configHash: "abc",
      running: true,
    })
    fake(mine, { config: "/proj/.mcpx.json", resolve: answer, servers: 1 })
    fake(other, { config: "/elsewhere/.mcpx.json", resolve: answer, servers: 9 })
    // The other daemon started later and has more servers, so every
    // tie-breaker points the wrong way. Only /v1/resolve knows better.
    writeInfo(state, "mine", {
      socket: mine,
      configPath: "/proj/.mcpx.json",
      startedAt: new Date(Date.now() - 86_400_000).toISOString(),
    })
    writeInfo(state, "other", {
      socket: other,
      configPath: "/elsewhere/.mcpx.json",
      startedAt: new Date().toISOString(),
    })

    const got = await discover({ env: envFor(state), bin: "", directory: "/proj/sub" })
    expect(got.reason).toBe("resolved")
    expect(got.rung).toBe(3)
    expect(got.ambiguous).toBe(false)
    expect(targetKey(got.target!)).toBe("unix://" + mine)
  })

  test("two daemons, no resolve route, no binary: it guesses and says so", async () => {
    const state = tempRoot()
    const mine = join(state, "d-mine.sock")
    const other = join(state, "d-other.sock")
    fake(mine, { config: "/proj/.mcpx.json", servers: 1 })
    fake(other, { config: "/elsewhere/.mcpx.json", servers: 9 })
    writeInfo(state, "mine", {
      socket: mine,
      configPath: "/proj/.mcpx.json",
      startedAt: new Date(Date.now() - 86_400_000).toISOString(),
    })
    writeInfo(state, "other", {
      socket: other,
      configPath: "/elsewhere/.mcpx.json",
      startedAt: new Date().toISOString(),
    })

    const got = await discover({ env: envFor(state), bin: "", directory: "/proj/sub" })
    // Never a failure, and never a refusal: something was chosen.
    expect(got.target).toBeDefined()
    expect(got.reason).toBe("guess")
    expect(got.rung).toBe(6)
    expect(got.ambiguous).toBe(true)
    expect(got.warning).toContain("2 daemons")
    // The config path being an ancestor of the directory outranks both
    // "started most recently" and "has the most servers".
    expect(targetKey(got.target!)).toBe("unix://" + mine)
    expect(got.candidates).toHaveLength(2)
  })
})

describe("rung 4: the binary", () => {
  const writeFakeCLI = (dir: string, socket: string): string => {
    const bin = join(dir, "fake-mcpx")
    writeFileSync(
      bin,
      `#!/bin/sh\necho '{"running":true,"socket":"${socket}","config":"/proj/.mcpx.json"}'\n`,
    )
    chmodSync(bin, 0o755)
    return bin
  }

  test("a binary is asked only when the files could not decide", async () => {
    const state = tempRoot()
    const mine = join(state, "d-mine.sock")
    const other = join(state, "d-other.sock")
    fake(mine, { config: "/proj/.mcpx.json" })
    fake(other, { config: "/elsewhere/.mcpx.json" })
    writeInfo(state, "mine", { socket: mine, configPath: "/proj/.mcpx.json" })
    writeInfo(state, "other", { socket: other, configPath: "/elsewhere/.mcpx.json" })

    // A real directory: execFile is given it as its cwd, and a cwd that does
    // not exist fails the spawn before the binary ever runs.
    const got = await discover({
      env: envFor(state),
      bin: writeFakeCLI(tempRoot(), mine),
      directory: tempRoot(),
    })
    expect(got.reason).toBe("cli")
    expect(got.rung).toBe(4)
    expect(targetKey(got.target!)).toBe("unix://" + mine)
  })

  test("a missing binary is not an error", async () => {
    const state = tempRoot()
    const got = await discover({
      env: envFor(state),
      bin: join(state, "does-not-exist"),
    })
    expect(got.reason).toBe("none")
  })

  test("binArgs is a vector, however it was written", () => {
    expect(splitArgs("--config /a/b")).toEqual(["--config", "/a/b"])
    expect(splitArgs('["--config","/a b/c"]')).toEqual(["--config", "/a b/c"])
    expect(splitArgs("")).toEqual([])
    expect(splitArgs(undefined)).toEqual([])
  })
})

describe("rung 0 and rung 1", () => {
  test("a named remote endpoint never falls back to a local daemon", async () => {
    const state = tempRoot()
    // A local daemon that would be chosen by every other rung.
    const local = join(state, "d-local.sock")
    fake(local, { config: "/proj/.mcpx.json" })
    writeInfo(state, "local", { socket: local, configPath: "/proj/.mcpx.json" })

    // Port 1 is not listening. Answering from the local daemon here would be
    // answering from the wrong machine, with the wrong servers.
    const got = await discover({
      env: { ...envFor(state), MCPX_DAEMON_ENDPOINT: "http://127.0.0.1:1" },
      bin: "",
      directory: "/proj",
    })
    expect(got.rung).toBe(0)
    expect(got.target).toEqual({ kind: "url", base: "http://127.0.0.1:1" })
    expect(got.warning).toContain("not answering")
  })

  test("an explicit socket wins over the scan", async () => {
    const state = tempRoot()
    const named = join(state, "d-named.sock")
    const scanned = join(state, "d-scanned.sock")
    fake(named)
    fake(scanned)
    writeInfo(state, "scanned", { socket: scanned })

    const got = await discover({ env: { ...envFor(state), MCPX_SOCKET: named }, bin: "" })
    expect(got.rung).toBe(0)
    expect(targetKey(got.target!)).toBe("unix://" + named)
  })

  test("a remembered daemon is used without a scan", async () => {
    const state = tempRoot()
    const socket = join(state, "d-rem.sock")
    const f = fake(socket)
    await writeRemembered({ MCPX_STATE_DIR: state }, "/proj", { kind: "socket", path: socket }, "indefinite")

    expect(readRemembered({ MCPX_STATE_DIR: state }, "/proj")).toEqual({
      kind: "socket",
      path: socket,
    })

    const got = await discover({ env: envFor(state), bin: "", directory: "/proj" })
    expect(got.rung).toBe(1)
    expect(got.reason).toBe("remembered")
    // Only the health check; no status, no resolve.
    expect(f.seen).toEqual(["/v1/health"])
  })

  test("a remembered daemon that is gone falls back to the ladder", async () => {
    const state = tempRoot()
    const gone = join(state, "d-gone.sock")
    const live = join(state, "d-live.sock")
    fake(live, { config: "/proj/.mcpx.json" })
    writeInfo(state, "live", { socket: live, configPath: "/proj/.mcpx.json" })
    await writeRemembered({ MCPX_STATE_DIR: state }, "/proj", { kind: "socket", path: gone }, "until-gone")

    const got = await discover({ env: envFor(state), bin: "", directory: "/proj" })
    expect(got.reason).toBe("only")
    expect(targetKey(got.target!)).toBe("unix://" + live)
  })

  test("a remembered choice belongs to one directory", async () => {
    const state = tempRoot()
    const socket = join(state, "d-rem.sock")
    await writeRemembered({ MCPX_STATE_DIR: state }, "/a", { kind: "socket", path: socket }, "indefinite")
    expect(readRemembered({ MCPX_STATE_DIR: state }, "/b")).toBeUndefined()
  })
})

describe("ranking", () => {
  const c = (over: Partial<Candidate>): Candidate => ({
    target: { kind: "socket", path: over.configPath ?? "/x" },
    source: "info",
    ...over,
  })

  test("an ancestor config path beats a newer daemon with more servers", () => {
    const ancestor = c({ configPath: "/proj/.mcpx.json", startedAt: "2020-01-01T00:00:00Z", servers: 1 })
    const newer = c({ configPath: "/other/.mcpx.json", startedAt: "2030-01-01T00:00:00Z", servers: 50 })
    expect(rank([newer, ancestor], "/proj/deep/inside")[0]).toBe(ancestor)
  })

  test("with no ancestor, the most recently started wins", () => {
    const old = c({ configPath: "/a/.mcpx.json", startedAt: "2020-01-01T00:00:00Z" })
    const recent = c({ configPath: "/b/.mcpx.json", startedAt: "2024-01-01T00:00:00Z" })
    expect(rank([old, recent], "/elsewhere")[0]).toBe(recent)
  })

  test("with neither, the one with the most servers wins", () => {
    const few = c({ configPath: "/a/.mcpx.json", servers: 1 })
    const many = c({ configPath: "/b/.mcpx.json", servers: 4 })
    expect(rank([few, many], "/elsewhere")[0]).toBe(many)
  })

  test("a config path that is a prefix but not an ancestor does not count", () => {
    // /proj-other is not inside /proj, however it sorts as a string.
    const trap = c({ configPath: "/proj-other/.mcpx.json", startedAt: "2030-01-01T00:00:00Z" })
    const real = c({ configPath: "/proj/.mcpx.json", startedAt: "2020-01-01T00:00:00Z" })
    expect(rank([trap, real], "/proj/sub")[0]).toBe(real)
  })
})

describe("goDurationMs", () => {
  test("parses the spelling the settings registry emits", () => {
    expect(goDurationMs("60s")).toBe(60_000)
    expect(goDurationMs("1m0s")).toBe(60_000)
    expect(goDurationMs("1m30s")).toBe(90_000)
    expect(goDurationMs("500ms")).toBe(500)
    expect(goDurationMs("2h")).toBe(7_200_000)
  })

  test("treats anything it does not understand as absent", () => {
    // A cooldown of NaN retries forever, which is the failure the caller
    // falls back from rather than into.
    expect(goDurationMs(undefined)).toBeUndefined()
    expect(goDurationMs("")).toBeUndefined()
    expect(goDurationMs("60")).toBeUndefined()
    expect(goDurationMs("soon")).toBeUndefined()
    expect(goDurationMs("60s please")).toBeUndefined()
    expect(goDurationMs("-30s")).toBeUndefined()
  })
})
