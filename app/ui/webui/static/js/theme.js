// Theme manager for the Simtezilo Web UI. Loaded synchronously in <head> so the
// theme is on <html> before the first paint (no flash of the wrong theme).
//
// Preference: 'dark' | 'light' | 'system' (stored in localStorage).
// Effective:  'dark' | 'light' (what is actually applied as data-bs-theme).
(function () {
    const STORAGE_KEY = 'simtezilo.theme';
    const DEFAULT_PREF = 'dark';
    const PREFS = ['dark', 'light', 'system'];

    let query = null;
    try {
        query = window.matchMedia('(prefers-color-scheme: dark)');
    } catch (e) {
        query = null;
    }

    function readStored() {
        try {
            const value = window.localStorage.getItem(STORAGE_KEY);
            return PREFS.indexOf(value) >= 0 ? value : DEFAULT_PREF;
        } catch (e) {
            return DEFAULT_PREF;
        }
    }

    function writeStored(pref) {
        try {
            window.localStorage.setItem(STORAGE_KEY, pref);
        } catch (e) {
            // Storage unavailable; the preference lasts for this page load only.
        }
    }

    let preference = readStored();

    function resolve(pref) {
        if (pref === 'system') {
            return query && !query.matches ? 'light' : 'dark';
        }
        return pref;
    }

    let effective = null;

    function apply(notify) {
        const next = resolve(preference);
        const changed = next !== effective;
        effective = next;
        document.documentElement.setAttribute('data-bs-theme', next);
        if (notify && changed) {
            document.dispatchEvent(new CustomEvent('themechange', {
                detail: { preference: preference, effective: effective }
            }));
        }
    }

    function set(pref) {
        if (PREFS.indexOf(pref) < 0) return;
        preference = pref;
        writeStored(pref);
        apply(true);
        // The preference may change without the effective theme changing
        // (e.g. dark -> system on a dark OS); UIs still need to update.
        document.dispatchEvent(new CustomEvent('themepreferencechange', {
            detail: { preference: preference, effective: effective }
        }));
    }

    if (query) {
        const onChange = function () {
            if (preference === 'system') apply(true);
        };
        if (query.addEventListener) {
            query.addEventListener('change', onChange);
        } else if (query.addListener) {
            query.addListener(onChange);
        }
    }

    window.simtezilo = window.simtezilo || {};
    window.simtezilo.theme = {
        get: function () { return preference; },
        set: set,
        effective: function () { return effective; }
    };

    apply(false);
})();
