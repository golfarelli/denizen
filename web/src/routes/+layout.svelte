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
	import { sidebarOpen, closeSidebar } from '$lib/sidebar';

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

	// Populates $me (used for the "Admin" link below, the storage indicator,
	// and the /admin page's own access check) whenever a session appears —
	// right after login, or on a reload that finds a token already in
	// localStorage.
	$effect(() => {
		if ($auth && $me === null) {
			refreshMe();
		}
	});

	// The drawer (mobile sidebar) has no reason to stay open across a
	// navigation — closing it here, rather than relying on each nav link's
	// own onclick, catches every way the route can change (a link inside
	// the drawer, but also back/forward or a redirect this layout itself
	// triggers, like the route guard above).
	$effect(() => {
		$page.url.pathname;
		closeSidebar();
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

	function formatGB(bytes: number): string {
		return (bytes / 1024 / 1024 / 1024).toFixed(1);
	}

	let quotaPercent = $derived(
		$me && $me.quota_bytes > 0 ? Math.min(100, ($me.storage_used_bytes / $me.quota_bytes) * 100) : 0
	);
	let initial = $derived($me?.username.charAt(0).toUpperCase() ?? '?');
</script>

<svelte:head>
	<link rel="icon" href={favicon} />
</svelte:head>

{#if $fullscreen}
	<main class="fullscreen">
		{@render children()}
	</main>
{:else if $auth}
	<div class="app-shell">
		<!-- svelte-ignore a11y_no_static_element_interactions -->
		<!-- svelte-ignore a11y_click_events_have_key_events -->
		<div class="sidebar-backdrop" class:visible={$sidebarOpen} onclick={closeSidebar}></div>

		<aside class="sidebar" class:open={$sidebarOpen}>
			<a class="brand" href="/">
				<svg width="22" height="22" viewBox="0 0 24 24" aria-hidden="true">
					<rect width="24" height="24" rx="6" fill="var(--color-accent)" />
					<path
						fill="var(--color-accent-contrast)"
						d="M6.5 7a1 1 0 0 1 1-1h3.2l1.3 1.3H17a1 1 0 0 1 1 1v7.2a1 1 0 0 1-1 1H7.5a1 1 0 0 1-1-1V7Z"
					/>
				</svg>
				Denizen
			</a>

			<nav class="sidebar-nav">
				<a href="/" class:active={$page.url.pathname === '/'}>
					<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
						<path d="M4 11.5 12 4l8 7.5" stroke-linecap="round" stroke-linejoin="round" />
						<path d="M6 10v9a1 1 0 0 0 1 1h3v-5h4v5h3a1 1 0 0 0 1-1v-9" stroke-linecap="round" stroke-linejoin="round" />
					</svg>
					Home
				</a>
				<a href="/shares" class:active={$page.url.pathname === '/shares'}>
					<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
						<circle cx="6" cy="12" r="2.2" />
						<circle cx="17" cy="6" r="2.2" />
						<circle cx="17" cy="18" r="2.2" />
						<path d="M8 10.8 15 7M8 13.2 15 17" stroke-linecap="round" />
					</svg>
					My shares
				</a>
				<a href="/trash" class:active={$page.url.pathname === '/trash'}>
					<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
						<path
							d="M5 7h14M9 7V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2M7 7l1 13a1 1 0 0 0 1 1h6a1 1 0 0 0 1-1l1-13"
							stroke-linecap="round"
							stroke-linejoin="round"
						/>
					</svg>
					Trash
				</a>
				{#if $me?.is_admin}
					<a href="/admin" class:active={$page.url.pathname === '/admin'}>
						<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
							<path d="M4 6h6M14 6h6M4 12h10M18 12h2M4 18h4M12 18h8" stroke-linecap="round" />
							<circle cx="12" cy="6" r="2" fill="var(--color-sidebar-bg)" />
							<circle cx="16" cy="12" r="2" fill="var(--color-sidebar-bg)" />
							<circle cx="8" cy="18" r="2" fill="var(--color-sidebar-bg)" />
						</svg>
						Admin
					</a>
				{/if}
			</nav>

			{#if $me}
				<div class="storage-indicator">
					<div class="storage-bar">
						<div
							class="storage-bar-fill"
							class:storage-full={quotaPercent >= 90}
							style="width: {quotaPercent}%"
						></div>
					</div>
					<span class="storage-label">
						{formatGB($me.storage_used_bytes)} GB of {formatGB($me.quota_bytes)} GB
					</span>
				</div>
			{/if}

			<button class="btn sidebar-logout" onclick={handleLogout}>
				<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
					<path
						d="M9 4H6a1 1 0 0 0-1 1v14a1 1 0 0 0 1 1h3M15 12H5M12 8l4 4-4 4"
						stroke-linecap="round"
						stroke-linejoin="round"
					/>
				</svg>
				Log out
			</button>
		</aside>

		<div class="app-main">
			<header class="topbar">
				<button class="hamburger" aria-label="Open menu" onclick={() => sidebarOpen.set(!$sidebarOpen)}>
					<svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
						<path d="M4 7h16M4 12h16M4 17h16" stroke-linecap="round" />
					</svg>
				</button>
				<a class="brand mobile-brand" href="/">Denizen</a>
				<div class="topbar-spacer"></div>
				{#if $me}
					<span class="user-chip">
						<span class="user-avatar">{initial}</span>
						{$me.username}
					</span>
				{/if}
			</header>
			<main class="with-topbar">
				{@render children()}
			</main>
		</div>
	</div>
{:else}
	<main>
		{@render children()}
	</main>
{/if}
