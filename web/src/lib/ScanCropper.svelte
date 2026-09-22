<script lang="ts">
	import { t } from '$lib/i18n';
	import { clampPoint, isConvexQuad, type Point, type Quad } from '$lib/scanGeometry';

	// The scanner's crop step: the photo with the page's four corners marked,
	// each a handle to drag (mouse, finger, or arrow keys) onto the real
	// corner. The corners start where automatic detection put them (or at a
	// default inset) — see ScanDialog.svelte, which owns that and the actual
	// warp. All coordinates here are in the photo's own pixel space (`width` x
	// `height`); the SVG's viewBox is that same space, so screen size never
	// enters into where a corner really is.
	let {
		src,
		width,
		height,
		quad = $bindable(),
		onadjust
	}: {
		src: string;
		width: number;
		height: number;
		quad: Quad;
		/** Called when the user moves a corner themselves — so a detection result that lands afterwards doesn't overwrite it. */
		onadjust?: () => void;
	} = $props();

	const CORNERS = ['tl', 'tr', 'br', 'bl'] as const;

	let svgEl = $state<SVGSVGElement>();
	let shownWidth = $state(0); // rendered width in CSS px — to keep handles a constant on-screen size
	let dragging = $state<number | null>(null);

	// 1 CSS px, in photo pixels.
	let unit = $derived(shownWidth > 0 ? width / shownWidth : 1);
	let valid = $derived(isConvexQuad(quad));
	let outline = $derived(quad.map((p) => `${p.x},${p.y}`).join(' '));
	// Everything outside the quad dimmed: the full frame, then the quad cut out of it (evenodd).
	let dim = $derived(
		`M0 0H${width}V${height}H0Z M${quad[0].x} ${quad[0].y}L${quad[1].x} ${quad[1].y}L${quad[2].x} ${quad[2].y}L${quad[3].x} ${quad[3].y}Z`
	);

	function moveCorner(index: number, point: Point) {
		const next = clampPoint(point, width, height);
		quad = quad.map((p, i) => (i === index ? next : p)) as Quad;
		onadjust?.();
	}

	function startDrag(index: number, e: PointerEvent) {
		dragging = index;
		(e.currentTarget as Element).setPointerCapture(e.pointerId);
		e.preventDefault(); // no scrolling / text selection while a finger is on a handle
	}

	function drag(index: number, e: PointerEvent) {
		if (dragging !== index || !svgEl) return;
		const rect = svgEl.getBoundingClientRect();
		moveCorner(index, {
			x: ((e.clientX - rect.left) / rect.width) * width,
			y: ((e.clientY - rect.top) / rect.height) * height
		});
	}

	function endDrag() {
		dragging = null;
	}

	function nudge(index: number, e: KeyboardEvent) {
		const step = Math.max(1, Math.round(Math.max(width, height) / 200)) * (e.shiftKey ? 5 : 1);
		const p = quad[index];
		const delta: Record<string, Point> = {
			ArrowLeft: { x: -step, y: 0 },
			ArrowRight: { x: step, y: 0 },
			ArrowUp: { x: 0, y: -step },
			ArrowDown: { x: 0, y: step }
		};
		const d = delta[e.key];
		if (!d) return;
		e.preventDefault();
		moveCorner(index, { x: p.x + d.x, y: p.y + d.y });
	}
</script>

<div
	class="scan-cropper"
	style:aspect-ratio="{width} / {height}"
	style:width="min(100%, calc(60dvh * {width} / {height}))"
	bind:clientWidth={shownWidth}
>
	<img {src} alt="" draggable="false" />
	<svg bind:this={svgEl} viewBox="0 0 {width} {height}" preserveAspectRatio="none" aria-hidden="false">
		<path d={dim} fill-rule="evenodd" class="scan-cropper-dim" />
		<polygon points={outline} class="scan-cropper-outline" class:invalid={!valid} vector-effect="non-scaling-stroke" />
		{#each quad as point, i (i)}
			<!-- A focusable slider per corner: the ARIA pattern for a handle
			     moved along two axes with the arrow keys. -->
			<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
			<g
				class="scan-handle"
				class:active={dragging === i}
				role="slider"
				tabindex="0"
				aria-label={$t(`dialogs.scan.corner.${CORNERS[i]}`)}
				aria-valuemin={0}
				aria-valuemax={width}
				aria-valuenow={Math.round(point.x)}
				aria-valuetext={`${Math.round(point.x)}, ${Math.round(point.y)}`}
				data-corner={CORNERS[i]}
				data-x={Math.round(point.x)}
				data-y={Math.round(point.y)}
				onpointerdown={(e) => startDrag(i, e)}
				onpointermove={(e) => drag(i, e)}
				onpointerup={endDrag}
				onpointercancel={endDrag}
				onkeydown={(e) => nudge(i, e)}
			>
				<!-- Generous invisible hit area (a finger is not a mouse pointer), small visible dot. -->
				<circle cx={point.x} cy={point.y} r={28 * unit} class="scan-handle-hit" />
				<circle cx={point.x} cy={point.y} r={9 * unit} class="scan-handle-dot" />
			</g>
		{/each}
	</svg>
</div>
