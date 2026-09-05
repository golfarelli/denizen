import { writable, derived, get } from 'svelte/store';
import en from './en.json';
import it from './it.json';

// A small hand-rolled i18n layer instead of a library (svelte-i18n etc.) —
// this app only ever needs two closely-related Latin languages with no
// plural rules or ICU MessageFormat, so a real i18n library would be a lot
// of dependency weight for {param} substitution this can do in a few
// lines. One flat JSON dictionary per locale (see en.json/it.json) is the
// "file per language" model this calls for — adding a third language
// later is just a new dictionary file plus one more entry in `dictionaries`
// and the selector's option list below (see routes/+layout.svelte).

export type Locale = 'it' | 'en';

const STORAGE_KEY = 'denizen.locale';

// en.json is the source of truth (every key is written in English first,
// then translated) — it also doubles as the fallback for any key a
// non-English dictionary hasn't caught up on yet, so a missing translation
// degrades to readable English instead of a raw key name.
const dictionaries: Record<Locale, Record<string, string>> = { en, it };

function loadInitial(): Locale {
	if (typeof localStorage === 'undefined') return 'it';
	const raw = localStorage.getItem(STORAGE_KEY);
	return raw === 'en' || raw === 'it' ? raw : 'it';
}

// Denizen's original deployment is a single Italian-speaking household —
// 'it' is the default for a visitor with no stored preference yet, unlike
// most i18n setups that default to English or the browser's own language.
export const locale = writable<Locale>(loadInitial());

locale.subscribe((value) => {
	if (typeof localStorage === 'undefined') return;
	localStorage.setItem(STORAGE_KEY, value);
});

function format(template: string, params?: Record<string, string | number>): string {
	if (!params) return template;
	return template.replace(/\{(\w+)\}/g, (match, key) => (key in params ? String(params[key]) : match));
}

function translate(loc: Locale, key: string, params?: Record<string, string | number>): string {
	const template = dictionaries[loc][key] ?? dictionaries.en[key] ?? key;
	return format(template, params);
}

// A reactive $t(key, params) usable directly in markup and in a
// component's own <script> code alike (Svelte's $store auto-subscription
// works in both) — mirrors svelte-i18n's own convention without the
// dependency.
export const t = derived(locale, ($locale) => (key: string, params?: Record<string, string | number>) =>
	translate($locale, key, params)
);

// Non-reactive escape hatch for code outside a component instance (a
// module-level function, not markup or a component's own script) where
// $t's auto-subscription doesn't apply.
export function tSync(key: string, params?: Record<string, string | number>): string {
	return translate(get(locale), key, params);
}
