import { writable } from 'svelte/store';

// Promise-based replacements for window.prompt()/confirm() — those are
// raw, unstyled browser dialogs, the one place left (secondbrain session
// 2026-08-16) where this app's chrome still looked jarringly unfinished
// next to everything else already styled to match. The actual <dialog>
// lives once in routes/+layout.svelte (GlobalDialog.svelte) — this store
// is how any component asks it to open without needing that component
// mounted as a child, same handoff shape as lib/newMenu.ts.

interface PromptRequest {
	kind: 'prompt';
	message: string;
	defaultValue: string;
	confirmLabel: string;
	resolve: (value: string | null) => void;
}

interface ConfirmRequest {
	kind: 'confirm';
	message: string;
	confirmLabel: string;
	danger: boolean;
	resolve: (value: boolean) => void;
}

export type DialogRequest = PromptRequest | ConfirmRequest;

export const dialogRequest = writable<DialogRequest | null>(null);

// Mirrors window.prompt(message, defaultValue): resolves to the entered
// text, or null if cancelled (Escape, the backdrop, or the Cancel button —
// GlobalDialog.svelte's own onclose handles all three the same way a real
// prompt() does). confirmLabel defaults to a plain "OK" but every real
// call site here passes something more specific ("Create", "Rename", ...)
// — a labeled action reads far better than a generic confirmation.
export function promptDialog(message: string, options: { defaultValue?: string; confirmLabel?: string } = {}): Promise<string | null> {
	return new Promise((resolve) => {
		dialogRequest.set({
			kind: 'prompt',
			message,
			defaultValue: options.defaultValue ?? '',
			confirmLabel: options.confirmLabel ?? 'OK',
			resolve
		});
	});
}

// Mirrors window.confirm(message): resolves true/false. danger tints the
// confirm button the same red the row-level danger actions already use
// (see .dropdown-menu button.danger), for anything destructive.
export function confirmDialog(message: string, options: { confirmLabel?: string; danger?: boolean } = {}): Promise<boolean> {
	return new Promise((resolve) => {
		dialogRequest.set({
			kind: 'confirm',
			message,
			confirmLabel: options.confirmLabel ?? 'OK',
			danger: options.danger ?? false,
			resolve
		});
	});
}
