<script lang="ts">
	// One node of the sidebar's folder tree (routes/+layout.svelte's "Home"
	// entry renders the root-level list, each item below that is one of
	// these, recursively — self-imported, same as any other recursive
	// Svelte component). Fetches its own children (reusing api.listItems —
	// the same endpoint the main file browser already calls — filtered to
	// folders) on every expand, not just the first one — see loadChildren's
	// own comment on why this isn't cached past that.
	import { untrack } from 'svelte';
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { api, type Item } from '$lib/api';
	import { autoExpandFolderIds, treeVersion } from '$lib/folderTree';
	import { t } from '$lib/i18n';
	import FileIcon from '$lib/FileIcon.svelte';
	import FolderTreeItem from '$lib/FolderTreeItem.svelte';

	let { item, depth }: { item: { id: string; name: string }; depth: number } = $props();

	let expanded = $state(false);
	// null = not fetched yet (distinct from [] = fetched, genuinely empty).
	let children = $state<Item[] | null>(null);
	let loading = $state(false);

	let active = $derived($page.url.pathname === '/' && $page.url.searchParams.get('folder') === item.id);

	// Refetches every time this is called, not just the first — a folder
	// created/renamed/moved/deleted elsewhere (the main file browser, the
	// trash page, ...) would otherwise leave an already-expanded branch
	// showing whatever was true the one time this ran. Only the in-flight
	// guard stays, to dedupe an auto-expand and a manual click landing at
	// once, not to skip a genuinely fresh fetch.
	async function loadChildren() {
		if (loading) return;
		loading = true;
		try {
			const all = await api.listItems(item.id);
			children = all.filter((i) => i.type === 'folder');
		} catch {
			// A folder we can no longer reach (deleted from under us) — leave
			// it collapsed-looking rather than surfacing a sidebar error for
			// what's a fairly minor, self-correcting nicety.
			children = [];
		} finally {
			loading = false;
		}
	}

	function toggle() {
		expanded = !expanded;
		if (expanded) loadChildren();
	}

	function open() {
		goto(`/?folder=${encodeURIComponent(item.id)}`);
	}

	// Reacts to routes/+layout.svelte's own ancestor walk: whenever the
	// current folder changes, every ancestor on the way to it gets added to
	// this set, so the node sitting on that path expands itself (and, once
	// expanded, loads its own children — cascading one level further down,
	// where the next ancestor's own node has just mounted and reacts the
	// same way) regardless of whether the user ever clicked its toggle.
	// untrack: see the effect below's own comment — loadChildren's guard
	// reads `loading` synchronously, which would otherwise make this effect
	// (mis)depend on it too.
	$effect(() => {
		if ($autoExpandFolderIds.has(item.id) && !expanded) {
			expanded = true;
			untrack(() => loadChildren());
		}
	});

	// A folder-affecting mutation happened somewhere (see lib/folderTree.ts)
	// — refetch, but only while this branch is actually open; a still-
	// collapsed one just picks up the change lazily next time it's expanded.
	// untrack: loadChildren's own guard reads `loading` synchronously
	// (before its first await) — left untracked, that read happens inside
	// *this* effect's own tracking window and gets picked up as one of its
	// dependencies, so the guard's own reset back to false once the fetch
	// finishes would re-trigger this same effect forever.
	$effect(() => {
		$treeVersion;
		if (expanded) untrack(() => loadChildren());
	});
</script>

<li class="tree-item">
	<div class="tree-row">
		<button
			class="tree-toggle"
			class:tree-toggle-expanded={expanded}
			style:margin-left="{depth}rem"
			aria-expanded={expanded}
			aria-label={expanded
				? $t('nav.collapseFolder', { name: item.name })
				: $t('nav.expandFolder', { name: item.name })}
			onclick={(e) => {
				e.stopPropagation();
				toggle();
			}}
		>
			<svg width="10" height="10" viewBox="0 0 24 24" aria-hidden="true">
				<path d="M9 5l7 7-7 7" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" />
			</svg>
		</button>
		<button class="tree-name" class:active onclick={open}>
			<FileIcon type="folder" name={item.name} size="1.1rem" />
			<span class="tree-name-text">{item.name}</span>
		</button>
	</div>
	{#if expanded}
		<ul class="tree-children">
			{#if loading && children === null}
				<li class="tree-loading" style:margin-left="{depth + 1}rem">{$t('common.loading')}</li>
			{:else if children}
				{#each children as child (child.id)}
					<FolderTreeItem item={child} depth={depth + 1} />
				{/each}
			{/if}
		</ul>
	{/if}
</li>
