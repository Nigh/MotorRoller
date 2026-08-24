export type WaveSample = {
	t: number
	a: number
	b: number
	c: number
	id: number
	iq: number
	iqRef: number
	uq: number
}

export type DutyKey = "a" | "b" | "c"
export type CurKey = "id" | "iq" | "iqRef" | "uq"
export type WaveKey = DutyKey | CurKey

/** Visible strip length in device-seq units (~1 ms/tick at 1 kHz). */
export const WINDOW_MS = 6000
/** Keep playhead this far behind the newest sample. */
export const FIXED_DELAY_MS = 55
export const MIN_BUFFER_MS = 30
export const MAX_BUFFER_MS = 90

/** Unwrap uint16 seq into a monotonic device timeline (~ms at 1 kHz). */
export class SeqClock {
	private lastRaw = 0
	private total = 0
	private primed = false

	reset(): void {
		this.lastRaw = 0
		this.total = 0
		this.primed = false
	}

	unwrap(raw: number): number {
		if (!this.primed) {
			this.primed = true
			this.lastRaw = raw & 0xffff
			this.total = 0
			return 0
		}
		let d = (raw & 0xffff) - this.lastRaw
		if (d < -32768) d += 65536
		if (d > 32768) d -= 65536
		this.lastRaw = raw & 0xffff
		this.total += d
		return this.total
	}
}

export type Playhead = {
	t: number
	ready: boolean
	lastWall: number
	/** device-seq units per wall-clock ms; ~1 at 1 kHz telemetry */
	seqPerMs: number
}

export function newPlayhead(): Playhead {
	return { t: 0, ready: false, lastWall: 0, seqPerMs: 1 }
}

export function resetPlayhead(p: Playhead): void {
	p.t = 0
	p.ready = false
	p.lastWall = 0
	p.seqPerMs = 1
}

/** EMA-update seq/wall rate from a burst (Δseq over Δwall). */
export function noteBurstRate(p: Playhead, seqDelta: number, wallDeltaMs: number): void {
	// Ignore pathological intervals — a 2ms emit with Δseq=20 would push rate to 10× and overrun.
	if (seqDelta <= 0 || wallDeltaMs < 5 || wallDeltaMs > 200) return
	const inst = seqDelta / wallDeltaMs
	if (inst < 0.25 || inst > 2.5) return
	p.seqPerMs = p.seqPerMs * 0.9 + inst * 0.1
	if (p.seqPerMs < 0.5) p.seqPerMs = 0.5
	if (p.seqPerMs > 1.5) p.seqPerMs = 1.5
}

/**
 * Advance playhead in device time, always staying behind the newest sample.
 * Overrunning latestT made sampleAt() hold → flat right edge, then prune wiped the strip.
 */
export function advancePlayhead(p: Playhead, now: number, latestT: number): number {
	const target = latestT - FIXED_DELAY_MS
	if (!p.ready) {
		p.t = target
		p.ready = true
		p.lastWall = now
		return p.t
	}
	const dt = Math.max(0, Math.min(40, now - p.lastWall))
	p.lastWall = now

	const maxT = latestT - MIN_BUFFER_MS
	const room = maxT - p.t
	if (room <= 0) {
		// Pause (or ease back) — never draw past available data
		p.t = maxT
		return p.t
	}
	const advance = Math.min(dt * p.seqPerMs, room)
	p.t += advance

	// If we fell far behind (USB stall recovery), ease toward the delay target
	const minT = latestT - MAX_BUFFER_MS
	if (p.t < minT) {
		p.t += 0.2 * (minT - p.t)
	}
	if (p.t > maxT) p.t = maxT
	return p.t
}

export function pruneSamples(samples: WaveSample[], playhead: number, latestT: number): void {
	// Anchor prune to data, not a runaway playhead
	const right = Math.min(playhead, latestT)
	const cut = right - WINDOW_MS - 100
	let n = 0
	while (n < samples.length && samples[n].t < cut) n++
	if (n > 0) samples.splice(0, n)
}

function catmull(p0: number, p1: number, p2: number, p3: number, u: number): number {
	const u2 = u * u
	const u3 = u2 * u
	return 0.5 * (2 * p1 + (-p0 + p2) * u + (2 * p0 - 5 * p1 + 4 * p2 - p3) * u2 + (-p0 + 3 * p1 - 3 * p2 + p3) * u3)
}

function ch(s: WaveSample, key: WaveKey): number {
	return s[key]
}

function isDuty(key: WaveKey): key is DutyKey {
	return key === "a" || key === "b" || key === "c"
}

export function sampleAt(samples: WaveSample[], t: number, key: WaveKey): number {
	const n = samples.length
	if (n === 0) return 0
	if (t <= samples[0].t) return ch(samples[0], key)
	if (t >= samples[n - 1].t) return ch(samples[n - 1], key)

	let lo = 0
	let hi = n - 1
	while (lo + 1 < hi) {
		const mid = (lo + hi) >> 1
		if (samples[mid].t <= t) lo = mid
		else hi = mid
	}
	const p1 = samples[lo]
	const p2 = samples[hi]
	const span = p2.t - p1.t
	const u = span <= 0 ? 1 : (t - p1.t) / span
	const p0 = samples[Math.max(0, lo - 1)]
	const p3 = samples[Math.min(n - 1, hi + 1)]
	const v = catmull(ch(p0, key), ch(p1, key), ch(p2, key), ch(p3, key), u)
	return isDuty(key) ? Math.min(1, Math.max(0, v)) : v
}

/**
 * Min/max over a time bin so narrow peaks are not lost when the strip scrolls
 * (one sample per pixel would alias spikes in and out of view).
 */
export function rangeIn(
	samples: WaveSample[],
	t0: number,
	t1: number,
	key: WaveKey,
): { min: number; max: number } {
	if (t1 < t0) {
		const tmp = t0
		t0 = t1
		t1 = tmp
	}
	const n = samples.length
	if (n === 0) return { min: 0, max: 0 }

	let lo = sampleAt(samples, t0, key)
	let hi = lo
	const v1 = sampleAt(samples, t1, key)
	if (v1 < lo) lo = v1
	if (v1 > hi) hi = v1

	// First index with t >= t0
	let i = 0
	let left = 0
	let right = n
	while (left < right) {
		const mid = (left + right) >> 1
		if (samples[mid].t < t0) left = mid + 1
		else right = mid
	}
	i = left
	while (i < n && samples[i].t <= t1) {
		const v = ch(samples[i], key)
		if (v < lo) lo = v
		if (v > hi) hi = v
		i++
	}
	if (isDuty(key)) {
		return {
			min: Math.min(1, Math.max(0, lo)),
			max: Math.min(1, Math.max(0, hi)),
		}
	}
	return { min: lo, max: hi }
}

/** Peak |Id|/|Iq|/|IqRef| over [t0,t1] for bipolar Y scale (never below floor). */
export function currentAmpScale(samples: WaveSample[], t0: number, t1: number, floor = 0.2): number {
	let peak = floor
	for (const key of ["id", "iq", "iqRef"] as const) {
		const { min, max } = rangeIn(samples, t0, t1, key)
		peak = Math.max(peak, Math.abs(min), Math.abs(max))
	}
	return peak
}

/** Seq ticks ≈ ms. Δmrad/Δt = rad/s → RPM. */
export function rpmFromDelta(dMrad: number, dtSeq: number): number {
	if (dtSeq <= 0) return 0
	return (dMrad / dtSeq) * (60 / (2 * Math.PI))
}

/** Rolling Δθ/Δt over ~200 seq ticks (device ms at 1 kHz). */
export class RpmMeter {
	private buf: { t: number; a: number }[] = []
	private readonly window = 200

	reset(): void {
		this.buf.length = 0
	}

	push(t: number, angleMrad: number): number {
		this.buf.push({ t, a: angleMrad })
		const cut = t - this.window
		while (this.buf.length > 2 && this.buf[0].t < cut) this.buf.shift()
		if (this.buf.length < 2) return 0
		const a0 = this.buf[0]
		const a1 = this.buf[this.buf.length - 1]
		return rpmFromDelta(a1.a - a0.a, a1.t - a0.t)
	}
}
