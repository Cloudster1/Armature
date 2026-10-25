import { useState } from "react";
import { IconButton, Tooltip } from "@/components/ui";
import { Icon } from "@/components/icons";
import { applyTheme, readTheme, type Theme } from "@/lib/theme";
import { t } from "@/i18n";

// Read while rendering, so the words follow the language.
const themeWord = (theme: Theme): string => (theme === "system" ? t.shell.theme.auto : theme === "light" ? t.shell.theme.light : t.shell.theme.dark);

/**
 * The light and dark switch: Auto, Light, Dark in turn. It says its word to
 * screen readers and to the tests, which read the button's text.
 */
export function ThemeButton({ side }: { side?: "top" | "bottom" | "right" }) {
  const [theme, setTheme] = useState<Theme>(readTheme);
  function cycle() {
    const next: Theme = theme === "system" ? "light" : theme === "light" ? "dark" : "system";
    setTheme(next);
    applyTheme(next);
  }
  const glyph = theme === "system" ? <Icon.Monitor /> : theme === "light" ? <Icon.Sun /> : <Icon.Moon />;
  return (
    <Tooltip text={t.shell.theme.tooltip(themeWord(theme))} side={side}>
      <IconButton icon={glyph} label={themeWord(theme)} size="sm" onClick={cycle} data-action="theme" data-guide="theme" />
    </Tooltip>
  );
}
