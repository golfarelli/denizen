<script lang="ts">
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { api, ApiError } from '$lib/api';
	import { setAuth } from '$lib/auth';

	let username = $state('');
	let password = $state('');
	let error = $state('');
	let loading = $state(false);

	// Lands back wherever the visitor actually came from — mainly
	// routes/s/[token]/+page.svelte sending someone here for a
	// requires_auth share it can't show them yet (see its own needsLogin
	// state). Only ever a same-app path: anything not starting with a
	// single "/" (a scheme-relative "//evil.com" included) is rejected
	// rather than trusted as a redirect target.
	function redirectTarget(): string {
		const then = $page.url.searchParams.get('then');
		if (then && then.startsWith('/') && !then.startsWith('//')) return then;
		return '/';
	}

	async function handleSubmit(event: SubmitEvent) {
		event.preventDefault();
		error = '';
		loading = true;
		try {
			const tokens = await api.login(username, password);
			setAuth({ accessToken: tokens.access_token, refreshToken: tokens.refresh_token });
			await goto(redirectTarget());
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Something went wrong.';
		} finally {
			loading = false;
		}
	}
</script>

<svelte:head>
	<title>Log in · Denizen</title>
</svelte:head>

<div class="auth-page">
	<div class="card">
		<h1>Denizen</h1>
		<p class="error-text" style:visibility={error ? 'visible' : 'hidden'}>{error || ' '}</p>
		<form onsubmit={handleSubmit}>
			<div class="field">
				<label for="username">Username</label>
				<input id="username" bind:value={username} autocomplete="username" required />
			</div>
			<div class="field">
				<label for="password">Password</label>
				<input
					id="password"
					type="password"
					bind:value={password}
					autocomplete="current-password"
					required
				/>
			</div>
			<button class="btn btn-primary" type="submit" disabled={loading}>
				{loading ? 'Logging in…' : 'Log in'}
			</button>
		</form>
		<p><a href="/register">Have an invite code? Create an account</a></p>
	</div>
</div>
