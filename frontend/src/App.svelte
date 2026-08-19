<script lang="ts">
	import { Events } from "@wailsio/runtime"
	import {
		ListDevices,
		Connect,
		Disconnect,
		SendCommand,
		ConnectedID,
	} from "../bindings/MotorRoller/appservice.js"

	type DeviceInfo = {
		id: string
		bus: number
		address: number
		manufacturer: string
		product: string
	}

	type Snapshot = {
		mode: number
		modeName: string
		angleMrad: number
		dutyA: number
		dutyB: number
		dutyC: number
		seq: number
		dutyAHist: number[]
		dutyBHist: number[]
		dutyCHist: number[]
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

	let pending: Snapshot | null = null
	let raf = 0

	let waveCanvas: HTMLCanvasElement | undefined = $state()

	const cmds = ["START", "STOP", "SPRING", "SPIN", "TEST"] as const
	const connected = $derived(connectedId !== "")

	const angleRad = $derived(angleMrad / 1000)
	const angleDeg = $derived((angleRad * 180) / Math.PI)
	const wrapRad = $derived((((angleRad % (Math.PI * 2)) + Math.PI * 2) % (Math.PI * 2)))
	const needleDeg = $derived((wrapRad * 180) / Math.PI)

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
		} catch (e) {
			statusMsg = String(e)
		} finally {
			busy = false
		}
	}

	async function send(cmd: string) {
		try {
			await SendCommand(cmd)
		} catch (e) {
			statusMsg = String(e)
		}
	}

	function scheduleFlush() {
		if (raf) return
		raf = requestAnimationFrame(() => {
			raf = 0
			const s = pending
			pending = null
			if (!s) return
			modeName = s.modeName
			angleMrad = s.angleMrad
			dutyA = s.dutyA
			dutyB = s.dutyB
			dutyC = s.dutyC
			seq = s.seq
			drawWave(s.dutyAHist, s.dutyBHist, s.dutyCHist)
		})
	}

	function drawWave(a: number[], b: number[], c: number[]) {
		const canvas = waveCanvas
		if (!canvas) return
		const dpr = window.devicePixelRatio || 1
		const w = canvas.clientWidth
		const h = canvas.clientHeight
		if (w === 0 || h === 0) return
		if (canvas.width !== Math.floor(w * dpr) || canvas.height !== Math.floor(h * dpr)) {
			canvas.width = Math.floor(w * dpr)
			canvas.height = Math.floor(h * dpr)
		}
		const ctx = canvas.getContext("2d")
		if (!ctx) return
		ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
		ctx.clearRect(0, 0, w, h)

		ctx.strokeStyle = "rgba(255,255,255,0.12)"
		ctx.beginPath()
		ctx.moveTo(0, h / 2)
		ctx.lineTo(w, h / 2)
		ctx.stroke()

		const series: [number[], string][] = [
			[a, "#f87171"],
			[b, "#4ade80"],
			[c, "#60a5fa"],
		]
		for (const [data, color] of series) {
			if (!data || data.length < 2) continue
			ctx.strokeStyle = color
			ctx.lineWidth = 1.5
			ctx.beginPath()
			for (let i = 0; i < data.length; i++) {
				const x = (i / (data.length - 1)) * w
				const y = h - data[i] * h
				if (i === 0) ctx.moveTo(x, y)
				else ctx.lineTo(x, y)
			}
			ctx.stroke()
		}
	}

	function pct(v: number): string {
		return (v * 100).toFixed(1) + "%"
	}

	$effect(() => {
		const off = Events.On("telemetry", (ev: { data?: Snapshot }) => {
			const data = Array.isArray(ev.data) ? ev.data[0] : ev.data
			if (!data) return
			pending = data as Snapshot
			scheduleFlush()
		})
		refreshDevices()
		ConnectedID().then((id: string) => {
			connectedId = id || ""
		})
		return () => {
			off?.()
			if (raf) cancelAnimationFrame(raf)
		}
	})
</script>

<main class="h-screen w-screen p-4 flex flex-col gap-3 text-left">
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

	<section class="grid grid-cols-1 lg:grid-cols-[1fr_240px] gap-3 flex-1 min-h-0">
		<div class="flex flex-col gap-2 min-h-0">
			<div class="flex flex-wrap gap-4 text-sm font-mono">
				<span class="text-red-400">A {pct(dutyA)}</span>
				<span class="text-green-400">B {pct(dutyB)}</span>
				<span class="text-blue-400">C {pct(dutyC)}</span>
				<span class="opacity-50">seq {seq}</span>
			</div>
			<div class="flex-1 min-h-[180px] rounded-box bg-base-200/60 border border-base-content/10 p-2">
				<canvas bind:this={waveCanvas} class="w-full h-full block"></canvas>
			</div>
		</div>

		<div class="flex flex-col items-center gap-2">
			<svg viewBox="0 0 200 200" class="w-52 h-52 shrink-0">
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
				<!-- needle: 0 rad = +X (right); SVG rotate is clockwise from +Y, so map wrapRad → CSS degrees from up -->
				<g transform="rotate({needleDeg - 90} 100 100)">
					<line x1="100" y1="100" x2="100" y2="24" stroke="#fbbf24" stroke-width="3" stroke-linecap="round" />
					<circle cx="100" cy="24" r="5" fill="#fbbf24" />
				</g>
			</svg>
			<div class="font-mono text-sm text-center leading-relaxed">
				<div>{angleDeg.toFixed(1)}°</div>
				<div class="opacity-70">{angleRad.toFixed(3)} rad</div>
				<div class="opacity-50">{angleMrad} mrad</div>
			</div>
		</div>
	</section>

	<footer class="flex flex-wrap items-center gap-2 border-t border-base-content/10 pt-3">
		<div class="mr-auto">
			<span class="text-xs opacity-60">Mode</span>
			<div class="font-mono text-lg">{modeName}</div>
		</div>
		{#each cmds as cmd}
			<button class="btn btn-sm" disabled={!connected || busy} onclick={() => send(cmd)}>{cmd}</button>
		{/each}
	</footer>
</main>
