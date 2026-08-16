// A deterministic color per username instead of every avatar being the
// same flat accent blue (secondbrain session 2026-08-16) — same idea
// Slack/Notion use for user avatars: a stable hash into a hue, so the
// same person always gets the same color across sessions/devices without
// storing anything.
export function avatarColor(username: string): string {
	let hash = 0;
	for (let i = 0; i < username.length; i++) {
		hash = (hash << 5) - hash + username.charCodeAt(i);
		hash |= 0; // keep it a 32-bit int, same as Java's String.hashCode()
	}
	const hue = Math.abs(hash) % 360;
	// Fixed saturation/lightness tuned for readable white text on top,
	// same contrast bar --color-accent already clears — only the hue varies.
	return `hsl(${hue} 55% 45%)`;
}
