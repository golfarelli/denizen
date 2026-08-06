<script lang="ts">
	import { onMount } from 'svelte';
	import '$lib/styles/app.css';
	import favicon from '$lib/assets/favicon.svg';
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { auth, clearAuth } from '$lib/auth';
	import { api } from '$lib/api';
	import { me, refreshMe, clearMe } from '$lib/me';
	import { registerServiceWorker } from '$lib/pwa';
	import { fullscreen } from '$lib/fullscreen';

	let { children } = $props();

	onMount(registerServiceWorker);

	const PUBLIC_ROUTES = ['/login', '/register'];

	// Route guard for this SPA: no server-side hook can do this (ssr is off
	// — see +layout.ts), so it happens client-side, on every navigation.
	$effect(() => {
		const isPublicRoute = PUBLIC_ROUTES.includes($page.url.pathname);
		if (!$auth && !isPublicRoute) {
			goto('/login');
		}
	});

	// Populates $me (used for the "Admin" link below and the /admin page's
	// own access check) whenever a session appears — right after login, or
	// on a reload that finds a token already in localStorage.
	$effect(() => {
		if ($auth && $me === null) {
			refreshMe();
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
		clearMe();
		await goto('/login');
	}
</script>

<svelte:head>
	<link rel="icon" href={favicon} />
</svelte:head>

{#if $auth && !$fullscreen}
	<header class="topbar">
		<a class="brand" href="/">Denizen</a>
		<nav style="display:flex; gap: var(--space-3); align-items:center">
			<a href="/shares">My shares</a>
			<a href="/trash">Trash</a>
			{#if $me?.is_admin}
				<a href="/admin">Admin</a>
			{/if}
			<button class="btn" onclick={handleLogout}>Log out</button>
		</nav>
	</header>
{/if}

<main class:with-topbar={!!$auth && !$fullscreen} class:fullscreen={$fullscreen}>
	{@render children()}
</main>
