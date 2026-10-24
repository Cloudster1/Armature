import { useMeta } from "@/api/meta";
import { COMMIT_SHORT_LENGTH } from "@/config";
import { useFormat } from "@/lib/format";
import { t } from "@/i18n";

/**
 * Which build is running, in one quiet line under the person. The short hash
 * is what someone reads out when reporting a problem; the whole of it, and
 * when it was built, sit in the title for the one who then looks it up.
 */
export function BuildStamp() {
  const { data } = useMeta();
  const format = useFormat();
  if (!data) return null;
  const { version, commit, builtAt } = data.build;
  const label = commit ? `${version} (${commit.slice(0, COMMIT_SHORT_LENGTH)})` : version;
  const detail = [commit && t.shell.build.commit(commit), builtAt && t.shell.build.built(format.dateTime(builtAt))].filter(Boolean).join(", ");
  return (
    <div className="truncate px-3 pb-2 text-2xs text-ink-subtle" title={detail || undefined} data-build>
      {label}
    </div>
  );
}
