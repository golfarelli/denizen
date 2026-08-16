<script lang="ts">
	import { onMount, untrack } from 'svelte';
	import '$lib/styles/app.css';
	import favicon from '$lib/assets/favicon.svg';
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { auth, clearAuth } from '$lib/auth';
	import { api, type Item } from '$lib/api';
	import { me, refreshMe, clearMe } from '$lib/me';
	import { registerServiceWorker } from '$lib/pwa';
	import { fullscreen } from '$lib/fullscreen';
	import { sidebarOpen, closeSidebar } from '$lib/sidebar';
	import { locale, t, type Locale } from '$lib/i18n';
	import { walkAncestors } from '$lib/ancestorChain';
	import { autoExpandFolderIds, treeVersion } from '$lib/folderTree';
	import FolderTreeItem from '$lib/FolderTreeItem.svelte';
	import { newMenuActions } from '$lib/newMenu';
	import GlobalDialog from '$lib/GlobalDialog.svelte';
	import { avatarColor } from '$lib/avatarColor';
	import { tabbarConfig, TAB_POOL } from '$lib/tabbarConfig';
	import TabIcon from '$lib/TabIcon.svelte';

	let { children } = $props();

	// The sidebar's own "+ New" trigger — Drive-style, first item, so it's
	// always the same tap regardless of which page you're on (secondbrain
	// session 2026-08-16, moved out of routes/+page.svelte's toolbar). What
	// it actually does is registered by whichever page supports it — see
	// lib/newMenu.ts's own doc comment; $newMenuActions is null on a page
	// that doesn't (Trash, Shares, ...), which hides this button entirely
	// rather than showing a trigger with nothing to do.
	let newMenuOpen = $state(false);

	// Mirrors routes/+page.svelte's own const of the same name — small
	// enough (just an i18n key lookup) that duplicating it here beats
	// threading a shared import in for three lines.
	const BLANK_DOCUMENT_MENU_LABEL_KEYS = {
		docx: 'fileBrowser.newDocxPlain',
		xlsx: 'fileBrowser.newXlsxPlain',
		pptx: 'fileBrowser.newPptxPlain'
	} as const;

	$effect(() => {
		if (!newMenuOpen) return;
		function handlePointerDown() {
			newMenuOpen = false;
		}
		function handleKeydown(e: KeyboardEvent) {
			if (e.key === 'Escape') newMenuOpen = false;
		}
		window.addEventListener('click', handlePointerDown);
		window.addEventListener('keydown', handleKeydown);
		return () => {
			window.removeEventListener('click', handlePointerDown);
			window.removeEventListener('keydown', handleKeydown);
		};
	});

	onMount(registerServiceWorker);

	const PUBLIC_ROUTES = ['/login', '/register'];

	// /s/[token] (a share landing page) is public too, but not a fixed
	// path like the two above — it's a whole dynamic segment, so it needs
	// its own check rather than a plain PUBLIC_ROUTES.includes(). A
	// requires_auth share still gets gated, just by that page itself (its
	// own needsLogin state, from a 401 on the metadata fetch) rather than
	// this blanket guard bouncing every visitor to /login before the page
	// even gets a chance to show what's actually being shared.
	function isPublicRoute(pathname: string): boolean {
		return PUBLIC_ROUTES.includes(pathname) || pathname.startsWith('/s/');
	}

	// Route guard for this SPA: no server-side hook can do this (ssr is off
	// — see +layout.ts), so it happens client-side, on every navigation.
	$effect(() => {
		if (!$auth && !isPublicRoute($page.url.pathname)) {
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

	// app.html hardcodes lang="en" (SvelteKit needs a static value there;
	// this app's own locale is a runtime choice — see lib/i18n's own
	// comment on why 'it' is actually the default). Left uncorrected, a
	// browser sees an Italian-language page declared as English and
	// reliably offers to auto-translate it — which then sweeps up the
	// "Denizen" brand name itself into translation along with everything
	// else, not something proper-noun content should ever go through.
	$effect(() => {
		document.documentElement.lang = $locale;
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

	// The sidebar's own folder tree — root-level folders eagerly (it's the
	// always-visible base of the tree), everything below that lazily inside
	// FolderTreeItem.svelte itself as each branch is actually expanded.
	let rootFolders = $state<Item[] | null>(null);
	let rootFoldersLoading = $state(false);
	let homeExpanded = $state(false);

	// Refetches every time Home is (re)expanded — not cached past that,
	// same as every other node (FolderTreeItem.svelte) — so a folder
	// created/renamed/moved elsewhere while collapsed shows up correctly
	// the next time it's opened instead of whatever was true the one time
	// this happened to run before.
	async function loadRootFolders() {
		if (rootFoldersLoading) return;
		rootFoldersLoading = true;
		try {
			const all = await api.listItems(null);
			rootFolders = all.filter((item) => item.type === 'folder');
		} catch {
			rootFolders = [];
		} finally {
			rootFoldersLoading = false;
		}
	}

	function toggleHome() {
		homeExpanded = !homeExpanded;
		if (homeExpanded) loadRootFolders();
	}

	// A folder-affecting mutation happened somewhere (see lib/folderTree.ts)
	// — refetch, but only while Home is actually open; a still-collapsed
	// tree just picks up the change lazily the next time it's expanded.
	// untrack: loadRootFolders' own guard reads rootFoldersLoading
	// synchronously (before its first await) — left untracked, that read
	// happens inside *this* effect's own tracking window and gets picked up
	// as one of its dependencies, so the guard's own reset back to false
	// once the fetch finishes would re-trigger this same effect forever.
	$effect(() => {
		$treeVersion;
		if (homeExpanded) untrack(() => loadRootFolders());
	});

	// Only meaningful on the file browser itself — the breadcrumb (routes/
	// +page.svelte) is the other thing scoped to this same route+param.
	let currentFolderId = $derived(
		$page.url.pathname === '/' ? $page.url.searchParams.get('folder') : null
	);

	// Reveals wherever the current folder actually is in the tree, however
	// you got there (typed a URL, opened a folder from search results, ...)
	// — not just navigation that started from the tree itself. Additive
	// only (never collapses anything the user already had open), so a
	// duplicate walk from quick back-to-back navigation is harmless even
	// without guarding against it landing out of order.
	$effect(() => {
		const folderId = currentFolderId;
		if (folderId) revealInTree(folderId);
	});

	async function revealInTree(folderId: string): Promise<void> {
		const chain = await walkAncestors(folderId);
		// Not ours (reached via a share) — the tree only ever shows the
		// caller's own drive, same scope as the "Home" nav item itself, so
		// there's nothing here to reveal.
		if (chain.length === 0 || !chain[0].owned) return;
		if (!homeExpanded) {
			homeExpanded = true;
			loadRootFolders();
		}
		autoExpandFolderIds.update((ids) => {
			const next = new Set(ids);
			for (const item of chain) {
				if (item.id !== folderId) next.add(item.id);
			}
			return next;
		});
	}

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

	function setLocale(next: Locale) {
		locale.set(next);
	}
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
				<span class="notranslate" translate="no">Denizen</span>
			</a>

			<nav class="sidebar-nav">
				{#if $newMenuActions?.canCreate}
					<div class="new-menu sidebar-new-menu">
						<button
							class="btn btn-primary"
							aria-haspopup="true"
							aria-expanded={newMenuOpen}
							onclick={(e) => {
								e.stopPropagation();
								newMenuOpen = !newMenuOpen;
							}}
						>
							<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
								<path d="M12 5v14M5 12h14" stroke-linecap="round" />
							</svg>
							{$t('fileBrowser.new')}
						</button>
						{#if newMenuOpen}
							<!-- svelte-ignore a11y_no_static_element_interactions -->
							<!-- svelte-ignore a11y_interactive_supports_focus -->
							<!-- svelte-ignore a11y_click_events_have_key_events -->
							<div class="dropdown-menu" onclick={(e) => e.stopPropagation()} role="menu">
								<button
									role="menuitem"
									onclick={() => {
										newMenuOpen = false;
										$newMenuActions?.upload();
									}}
								>
									<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
										<path d="M12 15V4M8 8l4-4 4 4M5 20h14" stroke-linecap="round" stroke-linejoin="round" />
									</svg>
									{$t('fileBrowser.uploadPlain')}
								</button>
								<button
									role="menuitem"
									onclick={() => {
										newMenuOpen = false;
										$newMenuActions?.scan();
									}}
								>
									<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
										<path d="M8 7l1.2-2h5.6L16 7h3a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V9a2 2 0 0 1 2-2h3Z" stroke-linejoin="round" />
										<circle cx="12" cy="13.5" r="3.2" />
									</svg>
									{$t('fileBrowser.scanPlain')}
								</button>
								<button
									role="menuitem"
									onclick={() => {
										newMenuOpen = false;
										$newMenuActions?.newFolder();
									}}
								>
									<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
										<path
											d="M4 6a2 2 0 0 1 2-2h4l2 2h6a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6Z"
											stroke-linejoin="round"
										/>
										<path d="M12 11v4M10 13h4" stroke-linecap="round" />
									</svg>
									{$t('fileBrowser.newFolderPlain')}
								</button>
								{#each ['docx', 'xlsx', 'pptx'] as const as ext}
									<button
										role="menuitem"
										onclick={() => {
											newMenuOpen = false;
											$newMenuActions?.newBlankDocument(ext);
										}}
									>
										<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
											<path d="M7 3h7l4 4v13a1 1 0 0 1-1 1H7a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1Z" stroke-linejoin="round" />
											<path d="M14 3v4h4" stroke-linejoin="round" />
										</svg>
										{$t(BLANK_DOCUMENT_MENU_LABEL_KEYS[ext])}
									</button>
								{/each}
							</div>
						{/if}
					</div>
				{/if}
				<!-- Home/Shares/Shared with me/Trash — hidden below the mobile
				     breakpoint (see app.css), where .bottom-tabbar now covers
				     Home plus whichever 3 of these (or Recent/Favorites) the
				     user picked in /settings (lib/tabbarConfig.ts) in one tap
				     instead of hamburger-then-tap; still the real nav on
				     desktop, which has no tab bar. Admin (below, outside this
				     wrapper) stays drawer-only everywhere — it's not part of
				     the tab bar's pool. -->
				<div class="sidebar-nav-primary">
				<div class="tree-root">
					<div class="tree-row">
						<button
							class="tree-toggle"
							class:tree-toggle-expanded={homeExpanded}
							aria-expanded={homeExpanded}
							aria-label={homeExpanded
								? $t('nav.collapseFolder', { name: $t('common.home') })
								: $t('nav.expandFolder', { name: $t('common.home') })}
							onclick={(e) => {
								e.stopPropagation();
								toggleHome();
							}}
						>
							<svg width="10" height="10" viewBox="0 0 24 24" aria-hidden="true">
								<path
									d="M9 5l7 7-7 7"
									fill="none"
									stroke="currentColor"
									stroke-width="2.4"
									stroke-linecap="round"
									stroke-linejoin="round"
								/>
							</svg>
						</button>
						<a href="/" class="tree-name" class:active={$page.url.pathname === '/' && !currentFolderId}>
							<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
								<path d="M4 11.5 12 4l8 7.5" stroke-linecap="round" stroke-linejoin="round" />
								<path d="M6 10v9a1 1 0 0 0 1 1h3v-5h4v5h3a1 1 0 0 0 1-1v-9" stroke-linecap="round" stroke-linejoin="round" />
							</svg>
							{$t('common.home')}
						</a>
					</div>
					{#if homeExpanded}
						<ul class="tree-children">
							{#if rootFoldersLoading && rootFolders === null}
								<li class="tree-loading" style:margin-left="1rem">{$t('common.loading')}</li>
							{:else if rootFolders}
								{#each rootFolders as folder (folder.id)}
									<FolderTreeItem item={folder} depth={1} />
								{/each}
							{/if}
						</ul>
					{/if}
				</div>
				<a href="/shares" class:active={$page.url.pathname === '/shares'}>
					<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
						<circle cx="6" cy="12" r="2.2" />
						<circle cx="17" cy="6" r="2.2" />
						<circle cx="17" cy="18" r="2.2" />
						<path d="M8 10.8 15 7M8 13.2 15 17" stroke-linecap="round" />
					</svg>
					{$t('nav.myShares')}
				</a>
				<a href="/shared-with-me" class:active={$page.url.pathname === '/shared-with-me'}>
					<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
						<circle cx="12" cy="8" r="3.2" />
						<path d="M5 20c0-3.9 3.1-7 7-7s7 3.1 7 7" stroke-linecap="round" stroke-linejoin="round" />
					</svg>
					{$t('nav.sharedWithMe')}
				</a>
				<a href="/trash" class:active={$page.url.pathname === '/trash'}>
					<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
						<path
							d="M5 7h14M9 7V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2M7 7l1 13a1 1 0 0 0 1 1h6a1 1 0 0 0 1-1l1-13"
							stroke-linecap="round"
							stroke-linejoin="round"
						/>
					</svg>
					{$t('nav.trash')}
				</a>
				</div>
				<!-- Outside .sidebar-nav-primary on purpose: unlike Home/Shares/
				     Shared with me/Trash, this one has no .bottom-tabbar
				     counterpart (that stayed scoped to the four it was built
				     for — see feature/bottom-tabbar), so it stays reachable via
				     the drawer at every width, mobile included. -->
				<a href="/recent" class:active={$page.url.pathname === '/recent'}>
					<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
						<circle cx="12" cy="12" r="9" />
						<path d="M12 7v5l3.5 2" stroke-linecap="round" stroke-linejoin="round" />
					</svg>
					{$t('nav.recent')}
				</a>
				<a href="/favorites" class:active={$page.url.pathname === '/favorites'}>
					<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
						<path d="M12 3.5l2.7 5.9 6.3.7-4.7 4.4 1.3 6.2-5.6-3.2-5.6 3.2 1.3-6.2-4.7-4.4 6.3-.7L12 3.5Z" stroke-linejoin="round" />
					</svg>
					{$t('nav.favorites')}
				</a>
				<a href="/settings" class:active={$page.url.pathname === '/settings'}>
					<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
						<circle cx="12" cy="12" r="3" />
						<path
							d="M19.4 13.5a1.7 1.7 0 0 0 .34 1.87l.06.06a2.06 2.06 0 1 1-2.92 2.92l-.06-.06a1.7 1.7 0 0 0-1.87-.34 1.7 1.7 0 0 0-1.03 1.56v.17a2.06 2.06 0 1 1-4.12 0v-.09a1.7 1.7 0 0 0-1.11-1.56 1.7 1.7 0 0 0-1.87.34l-.06.06a2.06 2.06 0 1 1-2.92-2.92l.06-.06a1.7 1.7 0 0 0 .34-1.87 1.7 1.7 0 0 0-1.56-1.03H4.5a2.06 2.06 0 1 1 0-4.12h.09a1.7 1.7 0 0 0 1.56-1.11 1.7 1.7 0 0 0-.34-1.87l-.06-.06a2.06 2.06 0 1 1 2.92-2.92l.06.06a1.7 1.7 0 0 0 1.87.34H10.5a1.7 1.7 0 0 0 1.03-1.56V4.5a2.06 2.06 0 1 1 4.12 0v.09a1.7 1.7 0 0 0 1.03 1.56 1.7 1.7 0 0 0 1.87-.34l.06-.06a2.06 2.06 0 1 1 2.92 2.92l-.06.06a1.7 1.7 0 0 0-.34 1.87V10.5a1.7 1.7 0 0 0 1.56 1.03h.17a2.06 2.06 0 1 1 0 4.12h-.09a1.7 1.7 0 0 0-1.56 1.03Z"
							stroke-linecap="round"
							stroke-linejoin="round"
						/>
					</svg>
					{$t('nav.settings')}
				</a>
				{#if $me?.is_admin}
					<a href="/admin" class:active={$page.url.pathname === '/admin'}>
						<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
							<path d="M4 6h6M14 6h6M4 12h10M18 12h2M4 18h4M12 18h8" stroke-linecap="round" />
							<circle cx="12" cy="6" r="2" fill="var(--color-sidebar-bg)" />
							<circle cx="16" cy="12" r="2" fill="var(--color-sidebar-bg)" />
							<circle cx="8" cy="18" r="2" fill="var(--color-sidebar-bg)" />
						</svg>
						{$t('nav.admin')}
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
						{$t('layout.storageLabel', { used: formatGB($me.storage_used_bytes), quota: formatGB($me.quota_bytes) })}
					</span>
				</div>
			{/if}

			<div class="language-toggle" role="group" aria-label="Language">
				<button
					class="language-toggle-btn"
					class:active={$locale === 'it'}
					aria-pressed={$locale === 'it'}
					onclick={() => setLocale('it')}
				>
					{$t('layout.languageItalian')}
				</button>
				<button
					class="language-toggle-btn"
					class:active={$locale === 'en'}
					aria-pressed={$locale === 'en'}
					onclick={() => setLocale('en')}
				>
					{$t('layout.languageEnglish')}
				</button>
			</div>

			<button class="btn sidebar-logout" onclick={handleLogout}>
				<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
					<path
						d="M9 4H6a1 1 0 0 0-1 1v14a1 1 0 0 0 1 1h3M15 12H5M12 8l4 4-4 4"
						stroke-linecap="round"
						stroke-linejoin="round"
					/>
				</svg>
				{$t('nav.logout')}
			</button>
		</aside>

		<div class="app-main">
			<header class="topbar">
				<button class="hamburger" aria-label={$t('layout.openMenu')} onclick={() => sidebarOpen.set(!$sidebarOpen)}>
					<svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
						<path d="M4 7h16M4 12h16M4 17h16" stroke-linecap="round" />
					</svg>
				</button>
				<a class="brand mobile-brand notranslate" href="/" translate="no">Denizen</a>
				<div class="topbar-spacer"></div>
				{#if $me}
					<span class="user-chip">
						<span class="user-avatar" style:background={avatarColor($me.username)}>{initial}</span>
						{$me.username}
					</span>
				{/if}
			</header>
			<main class="with-topbar">
				{@render children()}
			</main>

			<!-- Mobile only (see the media query in app.css) — Home plus the
			     3 destinations configured in /settings ($tabbarConfig,
			     lib/tabbarConfig.ts), reachable in one tap instead of
			     hamburger-then-tap. The sidebar itself
			     stays the nav on desktop, where there's no bottom tab bar and
			     screen width was never the constraint driving this. Hidden
			     during an active file-list selection via a :has() rule in
			     app.css — .selection-toolbar (routes/+page.svelte) already
			     owns that same screen edge then, same reasoning the FAB
			     already hides for. -->
			<nav class="bottom-tabbar" aria-label={$t('layout.openMenu')}>
				<a href="/" class:active={$page.url.pathname === '/' && !currentFolderId}>
					<svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
						<path d="M4 11.5 12 4l8 7.5" stroke-linecap="round" stroke-linejoin="round" />
						<path d="M6 10v9a1 1 0 0 0 1 1h3v-5h4v5h3a1 1 0 0 0 1-1v-9" stroke-linecap="round" stroke-linejoin="round" />
					</svg>
					{$t('common.home')}
				</a>
				{#each $tabbarConfig as key (key)}
					<a href={TAB_POOL[key].href} class:active={$page.url.pathname === TAB_POOL[key].href}>
						<TabIcon kind={key} />
						{$t(TAB_POOL[key].labelKey)}
					</a>
				{/each}
			</nav>
		</div>
	</div>
	<GlobalDialog />
{:else}
	<main>
		{@render children()}
	</main>
{/if}
