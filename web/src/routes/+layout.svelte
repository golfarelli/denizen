<script lang="ts">
	import '$lib/styles/app.css';
	import favicon from '$lib/assets/favicon.svg';
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { auth, clearAuth } from '$lib/auth';
	import { api } from '$lib/api';

	let { children } = $props();

	const PUBLIC_ROUTES = ['/login', '/register'];

	// Route guard for this SPA: no server-side hook can do this (ssr is off
	// — see +layout.ts), so it happens client-side, on every navigation.
	$effect(() => {
		const isPublicRoute = PUBLIC_ROUTES.includes($page.url.pathname);
		if (!$auth && !isPublicRoute) {
			goto('/login');
		}
	});

	async function handleLogout() {
		if ($auth) {
			await api.logout($auth.refreshToken).catch(() => {
				// Best-effort: the refresh token gets forgotten client-side
				// either way, so a failed revoke call shouldn't block logout.
			});
		}
		clearAuth();
		await goto('/login');
	}
</script>

<svelte:head>
	<link rel="icon" href={favicon} />
</svelte:head>

{#if $auth}
	<header class="topbar">
		<a class="brand" href="/">Denizen</a>
		<button class="btn" onclick={handleLogout}>Log out</button>
	</header>
{/if}

<main class:with-topbar={!!$auth}>
	{@render children()}
</main>
