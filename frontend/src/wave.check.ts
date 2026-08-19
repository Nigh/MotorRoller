import {
	FIXED_DELAY_MS,
	MIN_BUFFER_MS,
	SeqClock,
	advancePlayhead,
	newPlayhead,
	noteBurstRate,
		pruneSamples,
		rangeIn,
		resetPlayhead,
		sampleAt,
		type WaveSample,
	} from "./wave.ts"

function assert(cond: boolean, msg: string) {
	if (!cond) throw new Error(msg)
}

const samples: WaveSample[] = [
	{ t: 0, a: 0, b: 0, c: 0 },
	{ t: 100, a: 1, b: 0, c: 0 },
	{ t: 200, a: 0, b: 1, c: 0 },
	{ t: 300, a: 0, b: 0, c: 1 },
]

assert(sampleAt(samples, -10, "a") === 0, "hold before start")
assert(sampleAt(samples, 300, "c") === 1, "hold at end")
assert(Math.abs(sampleAt(samples, 50, "a") - 0.5) < 0.15, "mid-rise interpolates")
assert(sampleAt([{ t: 1, a: 0.25, b: 0, c: 0 }], 99, "a") === 0.25, "single sample")

const spiked: WaveSample[] = [
	{ t: 0, a: 0, b: 0, c: 0 },
	{ t: 50, a: 1, b: 0, c: 0 },
	{ t: 100, a: 0, b: 0, c: 0 },
]
const env = rangeIn(spiked, 0, 100, "a")
assert(env.max === 1 && env.min === 0, "bin envelope keeps spike")
const envShift = rangeIn(spiked, 10, 90, "a")
assert(envShift.max === 1, "spike still visible when not at bin edge")

const clock = new SeqClock()
assert(clock.unwrap(10) === 0, "first unwrap anchors at 0")
assert(clock.unwrap(15) === 5, "monotonic step")
const c2 = new SeqClock()
assert(c2.unwrap(65530) === 0, "anchor near end")
assert(c2.unwrap(65535) === 5, "step to max")
assert(c2.unwrap(2) === 8, "uint16 wrap")

const ph = newPlayhead()
const t0 = advancePlayhead(ph, 1000, 200)
assert(t0 === 200 - FIXED_DELAY_MS, "start behind latest by fixed delay")
const t1 = advancePlayhead(ph, 1016, 220)
assert(t1 > t0, "playhead advances with wall time")
assert(t1 <= 220 - MIN_BUFFER_MS, "never past latest-minBuffer")

// Force overrun attempt: seqPerMs high, tiny latest
ph.seqPerMs = 1.5
const latest = t1 + 10
const t2 = advancePlayhead(ph, 1100, latest)
assert(t2 <= latest - MIN_BUFFER_MS, "hard cap stops overrun")

resetPlayhead(ph)
noteBurstRate(ph, 20, 20)
assert(Math.abs(ph.seqPerMs - 1) < 0.2, "burst rate near 1")
const before = ph.seqPerMs
noteBurstRate(ph, 20, 2) // too short — ignored
assert(ph.seqPerMs === before, "reject tiny wall delta")

const strip: WaveSample[] = [
	{ t: 0, a: 0, b: 0, c: 0 },
	{ t: 100, a: 1, b: 0, c: 0 },
	{ t: 5000, a: 0.5, b: 0, c: 0 },
]
pruneSamples(strip, 10000, 5000) // runaway playhead must not wipe data still in window of latest
assert(strip.length >= 1 && strip[strip.length - 1].t === 5000, "prune uses min(playhead,latest)")

console.log("wave.check ok")
