import * as tus from 'tus-js-client';
import { get } from 'svelte/store';
import { auth } from './auth';
import { refreshAccessToken } from './api';

// tus-js-client is the official JS client for the same tus resumable-upload
// protocol the backend speaks via tusd (see internal/upload's package doc)
// — hand-rolling chunking, retry-with-backoff, and offset resumption would
// be exactly the "genuinely complex infrastructure" CONTRIBUTING.md already
// carves an exception for on the Go side; this is the same call, made for
// the same reason, on the client.

export interface UploadCallbacks {
	onProgress: (percent: number) => void;
	onSuccess: () => void;
	onError: (message: string) => void;
}

/** Starts (and returns) a resumable upload of file into parentId (null =
 * root). The upload's own Authorization header is set fresh on every HTTP
 * request tus-js-client makes (onBeforeRequest), not just once at start —
 * a large upload can easily outlive the access token's TTL (default 15
 * minutes), and re-reading the token store here is what keeps a long
 * upload from failing partway through purely because of that. */
export function startUpload(file: File, parentId: string | null, callbacks: UploadCallbacks): tus.Upload {
	const metadata: Record<string, string> = { filename: file.name };
	if (parentId) metadata.parent_id = parentId;
	if (file.type) metadata.filetype = file.type;

	const upload = new tus.Upload(file, {
		endpoint: '/api/v1/uploads/',
		metadata,
		retryDelays: [0, 1000, 3000, 5000],

		onBeforeRequest: (req) => {
			const state = get(auth);
			if (state?.accessToken) {
				req.setHeader('Authorization', `Bearer ${state.accessToken}`);
			}
		},

		onShouldRetry: (error, retryAttempt, options) => {
			const status = error.originalResponse?.getStatus();
			const attemptsLeft = retryAttempt < (options.retryDelays?.length ?? 0);
			if (status === 401) {
				// Refresh now so the *next* attempt's onBeforeRequest picks up a
				// fresh token; tus-js-client still waits out its own backoff
				// before actually retrying.
				void refreshAccessToken();
				return attemptsLeft;
			}
			if (status !== undefined && status >= 400 && status < 500) {
				return false; // a 4xx other than 401 (bad input, quota exceeded, ...) won't fix itself by retrying
			}
			return attemptsLeft;
		},

		onProgress: (bytesSent, bytesTotal) => {
			callbacks.onProgress(Math.round((bytesSent / bytesTotal) * 100));
		},
		onSuccess: () => callbacks.onSuccess(),
		onError: (error) => callbacks.onError(error.message)
	});

	upload.start();
	return upload;
}
