import {
	FIXED_DELAY_MS,
	MIN_BUFFER_MS,
	SeqClock,
	advancePlayhead,
	currentAmpScale,
	newPlayhead,
	noteBurstRate,
	pruneSamples,
	rangeIn,
	resetPlayhead,
	rpmFromDelta,
	RpmMeter,
	sampleAt,
	type WaveSample,
} from "./wave.ts"

function assert(cond: boolean, msg: string) {
	if (!cond) throw new Error(msg)
}

const z = { id: 0, iq: 0, iqRef: 0, uq: 0 }
const samples: WaveSample[] = [
	{ t: 0, a: 0, b: 0, c: 0, ...z },
	{ t: 100, a: 1, b: 0, c: 0, ...z },
	{ t: 200, a: 0, b: 1, c: 0, ...z },
	{ t: 300, a: 0, b: 0, c: 1, ...z },
]

assert(sampleAt(samples, -10, "a") === 0, "hold before start")
assert(sampleAt(samples, 300, "c") === 1, "hold at end")
assert(Math.abs(sampleAt(samples, 50, "a") - 0.5) < 0.15, "mid-rise interpolates")
assert(sampleAt([{ t: 1, a: 0.25, b: 0, c: 0, ...z }], 99, "a") === 0.25, "single sample")

// Spike at t=50 missed by center sample of a wide bin — envelope must still catch it
const spiked: WaveSample[] = [
	{ t: 0, a: 0, b: 0, c: 0, ...z },
	{ t: 50, a: 1, b: 0, c: 0, ...z },
	{ t: 100, a: 0, b: 0, c: 0, ...z },
]
const env = rangeIn(spiked, 0, 100, "a")
assert(env.max === 1 && env.min === 0, "bin envelope keeps spike")
const envShift = rangeIn(spiked, 10, 90, "a")
assert(envShift.max === 1, "spike still visible when not at bin edge")

const iqStrip: WaveSample[] = [
	{ t: 0, a: 0, b: 0, c: 0, id: 0, iq: -0.4, iqRef: 0, uq: 0 },
	{ t: 50, a: 0, b: 0, c: 0, id: 0, iq: 1.2, iqRef: 0.5, uq: -0.8 },
	{ t: 100, a: 0, b: 0, c: 0, id: 0, iq: 0, iqRef: 0, uq: 0 },
]
const iqEnv = rangeIn(iqStrip, 0, 100, "iq")
assert(iqEnv.max === 1.2 && iqEnv.min === -0.4, "bipolar iq envelope")
assert(currentAmpScale(iqStrip, 0, 100) === 1.2, "amp scale from peak")
assert(currentAmpScale(iqStrip, 0, 100, 2) === 2, "amp scale floor")

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

ph.seqPerMs = 1.5
const latest = t1 + 10
const t2 = advancePlayhead(ph, 1100, latest)
assert(t2 <= latest - MIN_BUFFER_MS, "hard cap stops overrun")

resetPlayhead(ph)
noteBurstRate(ph, 20, 20)
assert(Math.abs(ph.seqPerMs - 1) < 0.2, "burst rate near 1")
const before = ph.seqPerMs
noteBurstRate(ph, 20, 2)
assert(ph.seqPerMs === before, "reject tiny wall delta")

const strip: WaveSample[] = [
	{ t: 0, a: 0, b: 0, c: 0, ...z },
	{ t: 100, a: 1, b: 0, c: 0, ...z },
	{ t: 5000, a: 0.5, b: 0, c: 0, ...z },
]
pruneSamples(strip, 10000, 5000)
assert(strip.length >= 1 && strip[strip.length - 1].t === 5000, "prune uses min(playhead,latest)")

const twoPiMrad = 2 * Math.PI * 1000
assert(Math.abs(rpmFromDelta(twoPiMrad, 1000) - 60) < 0.01, "1 rev/s → 60 RPM")
assert(rpmFromDelta(100, 0) === 0, "zero dt")
const meter = new RpmMeter()
assert(meter.push(0, 0) === 0, "need two samples")
const rpm60 = meter.push(1000, twoPiMrad)
assert(Math.abs(rpm60 - 60) < 0.01, "meter 1 rev in 1s")

console.log("wave.check ok")
