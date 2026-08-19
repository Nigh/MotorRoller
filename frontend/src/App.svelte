<script lang="ts">
	import { Events } from "@wailsio/runtime"
	import {
		ListDevices,
		Connect,
		Disconnect,
		SendCommand,
		ConnectedID,
		Goto,
		SetK,
		SetRest,
	} from "../bindings/MotorRoller/appservice.js"
	import {
		WINDOW_MS,
		SeqClock,
		advancePlayhead,
		newPlayhead,
		noteBurstRate,
		pruneSamples,
		rangeIn,
		resetPlayhead,
		type WaveSample,
	} from "./wave"

	type DeviceInfo = {
		id: string
		bus: number
		address: number
		manufacturer: string
		product: string
	}

	type TelemPoint = {
		seq: number
		dutyA: number
		dutyB: number
		dutyC: number
	}

	type Snapshot = {
		mode: number
		modeName: string
		angleMrad: number
		dutyA: number
		dutyB: number
		dutyC: number
		seq: number
		points?: TelemPoint[]
	}

	let devices: DeviceInfo[] = $state([])
	let selectedId: string = $state("")
	let connectedId: string = $state("")
	let statusMsg: string = $state("")
	let busy: boolean = $state(false)

	let modeName: string = $state("—")
	let angleMrad: number = $state(0)
	let dutyA: number = $state(0)
	let dutyB: number = $state(0)
	let dutyC: number = $state(0)
	let seq: number = $state(0)

	let tracking = $state(false)
	let targetMrad = $state(0)
	let dragging = $state(false)
	let kx10 = $state(25) // K = 2.5 default

	let pending: Snapshot | null = null
	let uiRaf = 0
	let waveRaf = 0
	const samples: WaveSample[] = []
	const seqClock = new SeqClock()
	const playhead = newPlayhead()
	let lastBurstWall = 0
	let lastBurstSeq = 0
	let lastGotoAt = 0
	let gotoPending: number | null = null
	let gotoTimer: ReturnType<typeof setTimeout> | null = null
	let ignoreTrackPress = false

	let waveCanvas: HTMLCanvasElement | undefined = $state()
	let dialSvg: SVGSVGElement | undefined = $state()

	const cmds = ["START", "STOP", "SPRING", "SPIN", "TEST"] as const
	const connected = $derived(connectedId !== "")
	const twoPi = Math.PI * 2

	/** UI angle: 0 at 12 o'clock, clockwise positive (opposite firmware CCW). */
	function fwToUi(mrad: number): number {
		return -mrad
	}
	function wrap01(rad: number): number {
		return ((rad % twoPi) + twoPi) % twoPi
	}

	const angleRad = $derived(fwToUi(angleMrad) / 1000)
	const angleDeg = $derived((angleRad * 180) / Math.PI)
	const needleDeg = $derived((wrap01(angleRad) * 180) / Math.PI)

	const targetUiRad = $derived(fwToUi(targetMrad) / 1000)
	const targetNeedleDeg = $derived((wrap01(targetUiRad) * 180) / Math.PI)

	function resetWave(): void {
		samples.length = 0
		seqClock.reset()
		resetPlayhead(playhead)
		lastBurstWall = 0
		lastBurstSeq = 0
	}

	async function refreshDevices() {
		try {
			const list = (await ListDevices()) as DeviceInfo[]
			devices = list ?? []
			if (!selectedId && devices.length > 0) {
				selectedId = devices[0].id
			}
			if (selectedId && !devices.some((d) => d.id === selectedId)) {
				selectedId = devices[0]?.id ?? ""
			}
			statusMsg = devices.length === 0 ? "No TinyKnob devices found" : ""
		} catch (e) {
			statusMsg = String(e)
		}
	}

	async function doConnect() {
		if (!selectedId) return
		busy = true
		statusMsg = ""
		try {
			resetWave()
			tracking = false
			await Connect(selectedId)
			connectedId = selectedId
		} catch (e) {
			statusMsg = String(e)
			connectedId = ""
		} finally {
			busy = false
		}
	}

	async function doDisconnect() {
		busy = true
		try {
			await Disconnect()
			connectedId = ""
			modeName = "—"
			tracking = false
			resetWave()
		} catch (e) {
			statusMsg = String(e)
		} finally {
			busy = false
		}
	}

	async function send(cmd: string) {
		try {
			if (cmd === "STOP" || cmd === "START" || cmd === "SPRING" || cmd === "SPIN" || cmd === "TEST") {
				tracking = false
			}
			await SendCommand(cmd)
		} catch (e) {
			statusMsg = String(e)
		}
	}

	function flushGoto() {
		gotoTimer = null
		if (gotoPending === null) return
		const m = gotoPending
		gotoPending = null
		lastGotoAt = performance.now()
		Goto(m).catch((e: unknown) => {
			statusMsg = String(e)
		})
	}

	/** Stream GOTO; coalesce to ≤ ~200 Hz. */
	function queueGoto(mrad: number) {
		targetMrad = mrad
		gotoPending = mrad
		const now = performance.now()
		const wait = Math.max(0, 5 - (now - lastGotoAt))
		if (gotoTimer != null) return
		gotoTimer = setTimeout(flushGoto, wait)
	}

	function enterTracking() {
		if (!connected) return
		tracking = true
		targetMrad = angleMrad
		queueGoto(angleMrad)
	}

	/** Pointer → UI rad (0 at 12 o'clock, CW+). */
	function pointerUiRad(ev: PointerEvent): number | null {
		const svg = dialSvg
		if (!svg) return null
		const pt = svg.createSVGPoint()
		pt.x = ev.clientX
		pt.y = ev.clientY
		const ctm = svg.getScreenCTM()
		if (!ctm) return null
		const local = pt.matrixTransform(ctm.inverse())
		const dx = local.x - 100
		const dy = local.y - 100
		// atan2(dx, -dy): 12→0, 3→π/2, 6→π (CW)
		return wrap01(Math.atan2(dx, -dy))
	}

	function unwrapNear(wrappedRad: number, nearMrad: number): number {
		let near = nearMrad / 1000
		let cand = wrappedRad
		while (cand - near > Math.PI) cand -= twoPi
		while (near - cand > Math.PI) cand += twoPi
		return Math.round(cand * 1000)
	}

	/** UI CW angle → firmware CCW mrad near current target. */
	function uiRadToFwMrad(uiRad: number): number {
		const fwWrapped = wrap01(-uiRad)
		return unwrapNear(fwWrapped, targetMrad)
	}

	function onDialPointerDown(ev: PointerEvent) {
		if (!connected || busy) return
		ev.preventDefault()
		ev.stopPropagation()
		const el = ev.currentTarget as HTMLElement
		el.setPointerCapture?.(ev.pointerId)
		if (!tracking) {
			enterTracking()
			ignoreTrackPress = true
			dragging = false
			return
		}
		ignoreTrackPress = false
		dragging = true
		const ui = pointerUiRad(ev)
		if (ui != null) queueGoto(uiRadToFwMrad(ui))
	}

	function onDialPointerMove(ev: PointerEvent) {
		if (ignoreTrackPress || !dragging || !tracking) return
		const ui = pointerUiRad(ev)
		if (ui != null) queueGoto(uiRadToFwMrad(ui))
	}

	function onDialPointerUp(ev: PointerEvent) {
		ignoreTrackPress = false
		dragging = false
		try {
			;(ev.currentTarget as HTMLElement).releasePointerCapture?.(ev.pointerId)
		} catch {
			/* ignore */
		}
	}

	function onDialWheel(ev: WheelEvent) {
		if (!connected || !tracking) return
		ev.preventDefault()
		const step = Math.max(20, Math.min(200, Math.abs(ev.deltaY) * 2))
		// scroll up → UI angle increase → firmware decrease
		const uiDir = ev.deltaY > 0 ? -1 : 1
		queueGoto(targetMrad - Math.round(uiDir * step))
	}

	async function applyK() {
		try {
			await SetK(kx10)
		} catch (e) {
			statusMsg = String(e)
		}
	}

	async function applyRest() {
		try {
			await SetRest()
		} catch (e) {
			statusMsg = String(e)
		}
	}

	function scheduleFlush() {
		if (uiRaf) return
		uiRaf = requestAnimationFrame(() => {
			uiRaf = 0
			const s = pending
			pending = null
			if (!s) return
			modeName = s.modeName
			angleMrad = s.angleMrad
			dutyA = s.dutyA
			dutyB = s.dutyB
			dutyC = s.dutyC
			seq = s.seq
		})
	}

	function drawWave(ph: number) {
		const canvas = waveCanvas
		if (!canvas) return
		const dpr = window.devicePixelRatio || 1
		const cssW = canvas.clientWidth
		const cssH = canvas.clientHeight
		if (cssW === 0 || cssH === 0) return
		const dw = Math.max(1, Math.floor(cssW * dpr))
		const dh = Math.max(1, Math.floor(cssH * dpr))
		if (canvas.width !== dw || canvas.height !== dh) {
			canvas.width = dw
			canvas.height = dh
		}
		const ctx = canvas.getContext("2d")
		if (!ctx) return
		// Draw in device pixels so Y AA is stable (CSS-pixel * dpr transform causes 1px bounce)
		ctx.setTransform(1, 0, 0, 1, 0, 0)
		ctx.clearRect(0, 0, dw, dh)

		const token = (name: string) =>
			getComputedStyle(document.documentElement).getPropertyValue(name).trim()
		const mid = Math.round(dh / 2) + 0.5
		ctx.strokeStyle = token("--color-base-content")
		ctx.globalAlpha = 0.12
		ctx.lineWidth = 1
		ctx.beginPath()
		ctx.moveTo(0, mid)
		ctx.lineTo(dw, mid)
		ctx.stroke()
		ctx.globalAlpha = 1

		if (samples.length < 2) return

		const tLeft = ph - WINDOW_MS
		const pad = 2 * dpr
		const yScale = dh - pad * 2
		const lw = Math.max(1, Math.round(dpr))
		const series: ["a" | "b" | "c", string][] = [
			["a", token("--color-error")],
			["b", token("--color-success")],
			["c", token("--color-info")],
		]
		const nx = dw
		const dt = WINDOW_MS / nx
		for (const [key, color] of series) {
			ctx.strokeStyle = color
			ctx.lineWidth = lw
			ctx.lineJoin = "round"
			ctx.lineCap = "round"
			ctx.beginPath()
			for (let i = 0; i < nx; i++) {
				const t0 = tLeft + i * dt
				const t1 = t0 + dt
				const { min, max } = rangeIn(samples, t0, t1, key)
				const yHi = pad + (1 - max) * yScale
				const yLo = pad + (1 - min) * yScale
				const x = i + 0.5
				ctx.moveTo(x, yHi)
				ctx.lineTo(x, yLo === yHi ? yHi + 0.5 : yLo)
			}
			ctx.stroke()
		}
	}

	function waveTick() {
		waveRaf = requestAnimationFrame(waveTick)
		if (samples.length === 0) {
			drawWave(0)
			return
		}
		const now = performance.now()
		const latestT = samples[samples.length - 1].t
		const ph = advancePlayhead(playhead, now, latestT)
		pruneSamples(samples, ph, latestT)
		drawWave(ph)
	}

	function ingestSnapshot(s: Snapshot) {
		const wall = performance.now()
		const pts =
			s.points && s.points.length > 0
				? s.points
				: [{ seq: s.seq, dutyA: s.dutyA, dutyB: s.dutyB, dutyC: s.dutyC }]
		let firstT = 0
		let lastT = 0
		for (let i = 0; i < pts.length; i++) {
			const p = pts[i]
			const t = seqClock.unwrap(p.seq)
			if (i === 0) firstT = t
			lastT = t
			samples.push({ t, a: p.dutyA, b: p.dutyB, c: p.dutyC })
		}
		if (lastBurstWall > 0 && lastT > lastBurstSeq) {
			noteBurstRate(playhead, lastT - lastBurstSeq, wall - lastBurstWall)
		} else if (pts.length > 1 && lastT > firstT && lastBurstWall > 0) {
			noteBurstRate(playhead, lastT - firstT, wall - lastBurstWall)
		}
		lastBurstWall = wall
		lastBurstSeq = lastT
	}

	function pct(v: number): string {
		return (v * 100).toFixed(1) + "%"
	}

	function barWidth(v: number): string {
		return Math.min(100, Math.max(0, v * 100)).toFixed(1) + "%"
	}

	$effect(() => {
		const off = Events.On("telemetry", (ev: { data?: Snapshot }) => {
			const data = Array.isArray(ev.data) ? ev.data[0] : ev.data
			if (!data) return
			const s = data as Snapshot
			ingestSnapshot(s)
			pending = s
			scheduleFlush()
		})
		refreshDevices()
		ConnectedID().then((id: string) => {
			connectedId = id || ""
		})
		waveRaf = requestAnimationFrame(waveTick)
		return () => {
			off?.()
			if (uiRaf) cancelAnimationFrame(uiRaf)
			if (waveRaf) cancelAnimationFrame(waveRaf)
			if (gotoTimer != null) clearTimeout(gotoTimer)
		}
	})
</script>

<main class="h-screen w-screen p-4 flex flex-col gap-3 text-left bg-base-100 text-base-content font-sans">
	<header class="flex flex-wrap items-end gap-2">
		<div class="flex-1 min-w-[220px]">
			<label class="label py-0" for="dev">
				<span class="label-text text-xs opacity-70">USB device (VID 0xACDC / PID 0x4011)</span>
			</label>
			<select id="dev" class="select select-bordered select-sm w-full" bind:value={selectedId} disabled={connected || busy}>
				{#if devices.length === 0}
					<option value="">No devices</option>
				{:else}
					{#each devices as d}
						<option value={d.id}>
							{d.id} — {d.product || "VBTT"} ({d.manufacturer || "?"})
						</option>
					{/each}
				{/if}
			</select>
		</div>
		<button class="btn btn-sm" onclick={refreshDevices} disabled={busy}>Refresh</button>
		{#if connected}
			<button class="btn btn-sm btn-warning" onclick={doDisconnect} disabled={busy}>Disconnect</button>
		{:else}
			<button class="btn btn-sm btn-primary" onclick={doConnect} disabled={busy || !selectedId}>Connect</button>
		{/if}
		<span class="text-sm opacity-80 self-center">
			{#if connected}
				Connected: {connectedId}
			{:else}
				Disconnected
			{/if}
		</span>
	</header>

	{#if statusMsg}
		<div class="alert alert-warning text-sm py-2">{statusMsg}</div>
	{/if}

	<section class="grid grid-cols-1 lg:grid-cols-[minmax(0,1fr)_240px] grid-rows-[minmax(0,1fr)_auto] lg:grid-rows-1 gap-3 flex-1 min-h-0 overflow-hidden">
		<div class="flex flex-col gap-2 min-h-0 overflow-hidden">
			<div class="grid grid-cols-3 gap-2 w-full shrink-0">
				{#each [
					{ k: "A", v: dutyA, fill: "bg-error", ink: "text-error-content" },
					{ k: "B", v: dutyB, fill: "bg-success", ink: "text-success-content" },
					{ k: "C", v: dutyC, fill: "bg-info", ink: "text-info-content" },
				] as p}
					<div class="@container relative h-8 overflow-hidden rounded-box bg-base-300 select-none">
						<span class="pointer-events-none absolute inset-0 z-0 flex items-center justify-center font-mono text-sm text-base-content select-none">
							{p.k} {pct(p.v)}
						</span>
						<div class="absolute inset-y-0 left-0 z-10 overflow-hidden {p.fill}" style="width: {barWidth(p.v)}">
							<span class="pointer-events-none flex h-full w-[100cqw] items-center justify-center font-mono text-sm select-none {p.ink}">
								{p.k} {pct(p.v)}
							</span>
						</div>
					</div>
				{/each}
			</div>
			<div class="flex-1 min-h-0 overflow-hidden rounded-box bg-base-200/60 border border-base-content/10 p-2">
				<canvas bind:this={waveCanvas} class="w-full h-full block"></canvas>
			</div>
		</div>

		<div class="flex flex-col items-center gap-2 shrink-0 mt-4 lg:min-h-0 lg:overflow-auto">
			<!-- Outer pad so tracking ring is not clipped by overflow / the wave cell. -->
			<!-- svelte-ignore a11y_no_static_element_interactions -->
			<div
				class="p-1.5 rounded-full shrink-0"
				class:ring-2={tracking}
				class:ring-accent={tracking}
			>
			<div
				class="relative w-52 h-52 select-none overflow-hidden rounded-full"
				class:cursor-pointer={connected}
				class:cursor-grabbing={dragging}
				onpointerdown={onDialPointerDown}
				onpointermove={onDialPointerMove}
				onpointerup={onDialPointerUp}
				onpointercancel={onDialPointerUp}
				onwheel={onDialWheel}
			>
				<svg bind:this={dialSvg} viewBox="0 0 200 200" class="w-full h-full pointer-events-none">
					<circle cx="100" cy="100" r="88" fill="none" stroke="currentColor" stroke-opacity="0.2" stroke-width="2" />
					<circle cx="100" cy="100" r="4" fill="currentColor" />
					{#each [0, 90, 180, 270] as tick}
						<line
							x1="100"
							y1="18"
							x2="100"
							y2="28"
							stroke="currentColor"
							stroke-opacity="0.45"
							stroke-width="2"
							transform="rotate({tick} 100 100)"
						/>
					{/each}
					<!-- UI: 0 at 12 o'clock, CW+ (SVG rotate is CW) -->
					<g class="text-warning" transform="rotate({needleDeg} 100 100)">
						<line x1="100" y1="100" x2="100" y2="28" stroke="currentColor" stroke-width="3" stroke-linecap="round" />
						<circle cx="100" cy="28" r="5" fill="currentColor" />
					</g>
					{#if tracking}
						<g class="text-accent" transform="rotate({targetNeedleDeg} 100 100)">
							<line
								x1="100"
								y1="100"
								x2="100"
								y2="30"
								stroke="currentColor"
								stroke-width="2.5"
								stroke-linecap="round"
								stroke-dasharray="4 3"
							/>
							<circle cx="100" cy="30" r="6" fill="currentColor" stroke="currentColor" stroke-opacity="0.45" stroke-width="1" />
						</g>
					{/if}
				</svg>
			</div>
			</div>
			<div class="font-mono text-sm text-center leading-relaxed">
				<div>{angleDeg.toFixed(1)}°</div>
				<div class="opacity-70">{angleRad.toFixed(3)} rad</div>
				{#if tracking}
					<div class="text-accent text-xs mt-1">TRACK → {targetUiRad.toFixed(3)} rad</div>
					<div class="text-xs opacity-50">drag / wheel · STOP to exit</div>
				{:else}
					<div class="text-xs opacity-50 mt-1">click / drag dial to track</div>
				{/if}
			</div>
		</div>
	</section>

	<footer class="flex flex-col gap-2 border-t border-base-content/10 pt-3">
		<div class="flex flex-wrap items-center gap-2">
			<div class="mr-auto">
				<span class="text-xs opacity-60">Mode</span>
				<div class="font-mono text-lg">{modeName}</div>
			</div>
			<span class="font-mono text-xs opacity-40 mr-2">seq {seq}</span>
			{#each cmds as cmd}
				<button class="btn btn-sm" disabled={!connected || busy} onclick={() => send(cmd)}>{cmd}</button>
			{/each}
		</div>
		<div class="flex flex-wrap items-center gap-3">
			<label class="flex items-center gap-2 text-sm grow min-w-[200px] max-w-md">
				<span class="opacity-70 whitespace-nowrap">K {(kx10 / 10).toFixed(1)}</span>
				<input
					type="range"
					class="range range-sm range-primary grow"
					min="0"
					max="80"
					step="1"
					bind:value={kx10}
					disabled={!connected || busy}
				/>
			</label>
			<button class="btn btn-sm" disabled={!connected || busy} onclick={applyK}>SET_K</button>
			<button class="btn btn-sm" disabled={!connected || busy} onclick={applyRest}>SET_REST</button>
		</div>
	</footer>
</main>
