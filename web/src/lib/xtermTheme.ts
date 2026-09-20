export function readXtermTheme() {
  const css = getComputedStyle(document.documentElement);
  const v = (name: string) => css.getPropertyValue(name).trim();
  return {
    background: v("--c-canvas"),
    foreground: v("--c-text-primary"),
    cursor: v("--c-primary-text"),
    selectionBackground: v("--c-border-strong"),
  };
}
