<script lang="ts">
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { api, ApiError } from '$lib/api';
	import { setAuth } from '$lib/auth';
	import { t } from '$lib/i18n';

	// Prefilled from an admin-generated invite link (see routes/admin —
	// it builds exactly this ?code= shape), so following one doesn't also
	// require retyping the code by hand.
	let inviteCode = $state($page.url.searchParams.get('code') ?? '');
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
			error = err instanceof ApiError ? err.message : $t('common.somethingWentWrong');
		} finally {
			loading = false;
		}
	}
</script>

<svelte:head>
	<title>{$t('register.title')} · Denizen</title>
</svelte:head>

<div class="auth-page">
	<div class="card">
		<h1>{$t('register.heading')}</h1>
		<p class="error-text" style:visibility={error ? 'visible' : 'hidden'}>{error || ' '}</p>
		<form onsubmit={handleSubmit}>
			<div class="field">
				<label for="invite">{$t('register.inviteCode')}</label>
				<input id="invite" bind:value={inviteCode} required />
			</div>
			<div class="field">
				<label for="username">{$t('common.username')}</label>
				<input id="username" bind:value={username} autocomplete="username" required minlength="3" />
			</div>
			<div class="field">
				<label for="password">{$t('common.password')}</label>
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
				{loading ? $t('register.creating') : $t('register.submit')}
			</button>
		</form>
		<p><a href="/login">{$t('register.alreadyHaveAccount')}</a></p>
	</div>
</div>
