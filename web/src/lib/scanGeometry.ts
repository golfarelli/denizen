// Pure geometry for the document scanner's crop step (ScanCropper.svelte,
// scanDetect.ts, scanWarp.ts) — no DOM, no OpenCV, so it's the same
// whether the corners came from automatic edge detection or from someone's
// finger.

export interface Point {
	x: number;
	y: number;
}

/** Four corners in reading order: top-left, top-right, bottom-right, bottom-left. */
export type Quad = [Point, Point, Point, Point];

/** Orders four unordered points as [top-left, top-right, bottom-right, bottom-left]. */
export function orderCorners(points: Point[]): Quad {
	if (points.length !== 4) throw new Error('orderCorners needs exactly 4 points');
	// Classic trick: top-left has the smallest x+y, bottom-right the largest;
	// top-right has the smallest y-x, bottom-left the largest. Holds for any
	// convex quad that isn't rotated close to 45° — a document photo isn't.
	const sum = (p: Point) => p.x + p.y;
	const diff = (p: Point) => p.y - p.x;
	const tl = points.reduce((a, b) => (sum(b) < sum(a) ? b : a));
	const br = points.reduce((a, b) => (sum(b) > sum(a) ? b : a));
	const tr = points.reduce((a, b) => (diff(b) < diff(a) ? b : a));
	const bl = points.reduce((a, b) => (diff(b) > diff(a) ? b : a));
	return [{ ...tl }, { ...tr }, { ...br }, { ...bl }];
}

/** A rectangle inset from the image edges — where the corners start when
 * detection finds nothing (or hasn't finished yet). */
export function defaultQuad(width: number, height: number, marginFraction = 0.06): Quad {
	const mx = width * marginFraction;
	const my = height * marginFraction;
	return [
		{ x: mx, y: my },
		{ x: width - mx, y: my },
		{ x: width - mx, y: height - my },
		{ x: mx, y: height - my }
	];
}

export function clampPoint(p: Point, width: number, height: number): Point {
	return { x: Math.min(width, Math.max(0, p.x)), y: Math.min(height, Math.max(0, p.y)) };
}

function distance(a: Point, b: Point): number {
	return Math.hypot(a.x - b.x, a.y - b.y);
}

/** Area of a simple polygon (shoelace formula). */
export function polygonArea(points: Point[]): number {
	let sum = 0;
	for (let i = 0; i < points.length; i++) {
		const a = points[i];
		const b = points[(i + 1) % points.length];
		sum += a.x * b.y - b.x * a.y;
	}
	return Math.abs(sum) / 2;
}

/** True if the four points form a convex quad (no bow-tie, no dent) — the
 * only shape a perspective warp makes sense for. */
export function isConvexQuad(q: Quad): boolean {
	let sign = 0;
	for (let i = 0; i < 4; i++) {
		const a = q[i];
		const b = q[(i + 1) % 4];
		const c = q[(i + 2) % 4];
		const cross = (b.x - a.x) * (c.y - b.y) - (b.y - a.y) * (c.x - b.x);
		if (cross === 0) return false;
		const s = Math.sign(cross);
		if (sign === 0) sign = s;
		else if (s !== sign) return false;
	}
	return true;
}

/** The size the flattened page should have: each side gets the longer of
 * its two edges (so nothing is squashed), then everything is scaled down —
 * never up — so the longest side is at most maxLongSide. */
export function quadOutputSize(q: Quad, maxLongSide: number): { width: number; height: number } {
	const [tl, tr, br, bl] = q;
	const width = Math.max(distance(tl, tr), distance(bl, br));
	const height = Math.max(distance(tl, bl), distance(tr, br));
	const scale = Math.min(1, maxLongSide / Math.max(width, height, 1));
	return {
		width: Math.max(1, Math.round(width * scale)),
		height: Math.max(1, Math.round(height * scale))
	};
}

/**
 * Solves for the 3x3 homography H (h33 = 1) that maps each dst[i] to src[i]:
 *   x = (h11 u + h12 v + h13) / (h31 u + h32 v + 1)
 *   y = (h21 u + h22 v + h23) / (h31 u + h32 v + 1)
 * Returned as [h11, h12, h13, h21, h22, h23, h31, h32]. Mapping output
 * pixels *back* to the source (dst → src) is what lets the warp sample each
 * output pixel exactly once, with no holes.
 */
export function solveHomography(dst: Quad, src: Quad): number[] {
	// 8 equations, 8 unknowns: A·h = b, augmented as [A | b].
	const m: number[][] = [];
	for (let i = 0; i < 4; i++) {
		const { x: u, y: v } = dst[i];
		const { x, y } = src[i];
		m.push([u, v, 1, 0, 0, 0, -x * u, -x * v, x]);
		m.push([0, 0, 0, u, v, 1, -y * u, -y * v, y]);
	}
	// Gaussian elimination with partial pivoting.
	for (let col = 0; col < 8; col++) {
		let pivot = col;
		for (let r = col + 1; r < 8; r++) if (Math.abs(m[r][col]) > Math.abs(m[pivot][col])) pivot = r;
		if (Math.abs(m[pivot][col]) < 1e-12) throw new Error('degenerate quad: cannot solve homography');
		[m[col], m[pivot]] = [m[pivot], m[col]];
		for (let r = col + 1; r < 8; r++) {
			const f = m[r][col] / m[col][col];
			for (let c = col; c < 9; c++) m[r][c] -= f * m[col][c];
		}
	}
	const h = new Array<number>(8).fill(0);
	for (let r = 7; r >= 0; r--) {
		let s = m[r][8];
		for (let c = r + 1; c < 8; c++) s -= m[r][c] * h[c];
		h[r] = s / m[r][r];
	}
	return h;
}
