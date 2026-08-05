<script lang="ts">
	import { goto } from '$app/navigation';
	import { api, ApiError } from '$lib/api';
	import { setAuth } from '$lib/auth';

	let username = $state('');
	let password = $state('');
	let error = $state('');
	let loading = $state(false);

	async function handleSubmit(event: SubmitEvent) {
		event.preventDefault();
		error = '';
		loading = true;
		try {
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
