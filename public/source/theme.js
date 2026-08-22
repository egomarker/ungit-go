const ko = require('knockout');
const programEvents = require('ungit-program-events');
const storage = require('ungit-storage');
const themeColor = require('./theme-color');

const STORAGE_KEY = 'ungit-theme';
const PREFERENCES = ['system', 'dark', 'light'];

class Theme {
  constructor() {
    this.options = [
      { value: 'system', label: 'System' },
      { value: 'dark', label: 'Dark' },
      { value: 'light', label: 'Light' },
    ];
    this.colorScheme = window.matchMedia ? window.matchMedia('(prefers-color-scheme: dark)') : null;
    this.preference = ko.observable(this._initialPreference());
    this.resolvedTheme = ko.observable(this._resolveTheme());

    this.preference.subscribe((preference) => {
      if (!PREFERENCES.includes(preference)) {
        this.preference('system');
        return;
      }
      storage.setItem(STORAGE_KEY, preference);
      this._applyTheme(true);
    });

    const onColorSchemeChange = () => {
      if (this.preference() === 'system') this._applyTheme(true);
    };
    if (this.colorScheme && this.colorScheme.addEventListener) {
      this.colorScheme.addEventListener('change', onColorSchemeChange);
    } else if (this.colorScheme && this.colorScheme.addListener) {
      this.colorScheme.addListener(onColorSchemeChange);
    }

    this._applyTheme(false);
  }

  _initialPreference() {
    const initializedPreference = normalizePreference(window.__ungitThemePreference);
    if (initializedPreference) return initializedPreference;

    const storedPreference = normalizePreference(storage.getItem(STORAGE_KEY));
    if (storedPreference) return storedPreference;

    return normalizePreference(ungit.config.theme) || 'system';
  }

  _resolveTheme() {
    const preference = this.preference();
    if (preference === 'dark' || preference === 'light') return preference;
    return this.colorScheme && this.colorScheme.matches ? 'dark' : 'light';
  }

  _applyTheme(announce) {
    const resolvedTheme = this._resolveTheme();
    const changed = resolvedTheme !== this.resolvedTheme();
    this.resolvedTheme(resolvedTheme);
    document.documentElement.setAttribute('data-theme', resolvedTheme);
    this.refreshStylesheets();
    if (announce && changed) {
      programEvents.dispatch({ event: 'theme-changed', theme: resolvedTheme });
    }
  }

  refreshStylesheets() {
    const resolvedTheme = this.resolvedTheme();
    document.querySelectorAll('link[data-ungit-theme]').forEach((stylesheet) => {
      stylesheet.media = stylesheet.dataset.ungitTheme === resolvedTheme ? 'all' : 'not all';
    });
  }

  colorForRef(name) {
    return themeColor.colorForRef(name, this.resolvedTheme());
  }

  graphEdgeColor() {
    return themeColor.GRAPH_COLORS[this.resolvedTheme()].edge;
  }

  graphFallbackColor() {
    return themeColor.GRAPH_COLORS[this.resolvedTheme()].fallback;
  }

  graphAccentColor() {
    return themeColor.GRAPH_COLORS[this.resolvedTheme()].accent;
  }
}

function normalizePreference(preference) {
  return PREFERENCES.includes(preference) ? preference : null;
}

module.exports = new Theme();
