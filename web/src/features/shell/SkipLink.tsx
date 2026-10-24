import type { MouseEvent } from "react";
import { MAIN_ID } from "./state";
import { t } from "@/i18n";

/** The first stop of the keyboard on every page: past the navigation, to the content. Shown only when focused. */
export function SkipLink() {
  return (
    <a
      href={`#${MAIN_ID}`}
      onClick={skipToMain}
      className="sr-only z-60 rounded-control bg-surface-overlay px-3 py-2 text-sm font-medium text-ink shadow-2 focus:not-sr-only focus:fixed focus:top-2 focus:left-2"
      data-skip-link
    >
      {t.shell.nav.skipToContent}
    </a>
  );
}

// The router owns the address, so the skip link moves focus itself rather
// than letting a hash change reach the history.
function skipToMain(event: MouseEvent<HTMLAnchorElement>) {
  event.preventDefault();
  document.getElementById(MAIN_ID)?.focus();
}
