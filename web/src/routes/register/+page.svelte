<script lang="ts">
	import { goto } from '$app/navigation';
	import { api, ApiError } from '$lib/api';
	import { setAuth } from '$lib/auth';

	let inviteCode = $state('');
	let username = $state('');
	let password = $state('');
	let error = $state('');
	let loading = $state(false);

	async function handleSubmit(event: SubmitEvent) {
		event.preventDefault();
		error = '';
		loading = true;
		try {
			await api.register(inviteCode, username, password);
			// Registration doesn't itself return tokens (see
			// internal/handler/auth.go's Register) — log straight in with the
			// same credentials rather than sending the user back to /login to
			// type them again.
			const tokens = await api.login(username, password);
			setAuth({ accessToken: tokens.access_token, refreshToken: tokens.refresh_token });
			await goto('/');
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Something went wrong.';
		} finally {
			loading = false;
		}
	}
</script>

<svelte:head>
	<title>Create account · Denizen</title>
</svelte:head>

<div class="auth-page">
	<div class="card">
		<h1>Create your account</h1>
		<p class="error-text" style:visibility={error ? 'visible' : 'hidden'}>{error || ' '}</p>
		<form onsubmit={handleSubmit}>
			<div class="field">
				<label for="invite">Invite code</label>
				<input id="invite" bind:value={inviteCode} required />
			</div>
			<div class="field">
				<label for="username">Username</label>
				<input id="username" bind:value={username} autocomplete="username" required minlength="3" />
			</div>
			<div class="field">
				<label for="password">Password</label>
				<input
					id="password"
					type="password"
					bind:value={password}
					autocomplete="new-password"
					required
					minlength="8"
				/>
			</div>
			<button class="btn btn-primary" type="submit" disabled={loading}>
				{loading ? 'Creating account…' : 'Create account'}
			</button>
		</form>
		<p><a href="/login">Already have an account? Log in</a></p>
	</div>
</div>
