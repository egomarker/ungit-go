const md5 = require('blueimp-md5');

const MIN_GRAPH_CONTRAST = 3;
const GRAPH_COLORS = {
  dark: {
    background: '#252833',
    edge: '#6d7a89',
    fallback: '#6d7a89',
    accent: '#252833',
  },
  light: {
    background: '#f6f8fa',
    edge: '#758392',
    fallback: '#758392',
    accent: '#f6f8fa',
  },
};

function normalizeTheme(theme) {
  return theme === 'light' ? 'light' : 'dark';
}

function colorForRef(name, theme) {
  theme = normalizeTheme(theme);
  const hash = md5(String(name));
  const hue = (parseInt(hash.slice(0, 8), 16) / 0xffffffff) * 360;
  const saturation = 0.58 + (parseInt(hash.slice(8, 10), 16) / 255) * 0.24;
  const seed = parseInt(hash.slice(10, 12), 16) / 255;
  let lightness = theme === 'dark' ? 0.48 + seed * 0.22 : 0.24 + seed * 0.2;
  const direction = theme === 'dark' ? 0.01 : -0.01;
  const background = GRAPH_COLORS[theme].background;
  let color = rgbToHex(hslToRgb(hue, saturation, lightness));

  while (contrastRatio(color, background) < MIN_GRAPH_CONTRAST) {
    lightness = Math.max(0, Math.min(1, lightness + direction));
    color = rgbToHex(hslToRgb(hue, saturation, lightness));
    if (lightness === 0 || lightness === 1) break;
  }

  return color;
}

function contrastRatio(first, second) {
  const firstLuminance = relativeLuminance(hexToRgb(first));
  const secondLuminance = relativeLuminance(hexToRgb(second));
  const lighter = Math.max(firstLuminance, secondLuminance);
  const darker = Math.min(firstLuminance, secondLuminance);
  return (lighter + 0.05) / (darker + 0.05);
}

function hexToRgb(hex) {
  const normalized = hex.replace(/^#/, '');
  if (!/^[0-9a-f]{6}$/i.test(normalized)) throw new Error(`Invalid hex color: ${hex}`);
  return [0, 2, 4].map((offset) => parseInt(normalized.slice(offset, offset + 2), 16));
}

function relativeLuminance(rgb) {
  const channels = rgb.map((channel) => {
    const value = channel / 255;
    return value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * channels[0] + 0.7152 * channels[1] + 0.0722 * channels[2];
}

function hslToRgb(hue, saturation, lightness) {
  const chroma = (1 - Math.abs(2 * lightness - 1)) * saturation;
  const hueSegment = hue / 60;
  const intermediate = chroma * (1 - Math.abs((hueSegment % 2) - 1));
  let red = 0;
  let green = 0;
  let blue = 0;

  if (hueSegment < 1) {
    red = chroma;
    green = intermediate;
  } else if (hueSegment < 2) {
    red = intermediate;
    green = chroma;
  } else if (hueSegment < 3) {
    green = chroma;
    blue = intermediate;
  } else if (hueSegment < 4) {
    green = intermediate;
    blue = chroma;
  } else if (hueSegment < 5) {
    red = intermediate;
    blue = chroma;
  } else {
    red = chroma;
    blue = intermediate;
  }

  const match = lightness - chroma / 2;
  return [red, green, blue].map((channel) => Math.round((channel + match) * 255));
}

function rgbToHex(rgb) {
  return `#${rgb.map((channel) => channel.toString(16).padStart(2, '0')).join('')}`;
}

module.exports = {
  GRAPH_COLORS,
  MIN_GRAPH_CONTRAST,
  colorForRef,
  contrastRatio,
};
