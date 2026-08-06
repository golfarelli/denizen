import { writable } from 'svelte/store';

// Set by routes that want this app's own chrome (the top nav bar, mainly)
// out of the way — currently only the file preview page, whose viewers
// (OnlyOffice especially, but really any of them on a phone) benefit far
// more from real screen space than from Denizen's own nav being visible
// while looking at a file. See routes/+layout.svelte for how this is
// consumed.
export const fullscreen = writable(false);
