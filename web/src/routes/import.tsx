import { useRef, useState } from "react";
import { Link, createRoute } from "@tanstack/react-router";
import { projectRoute } from "./project";
import { useProject } from "@/api/projects";
import { useImportIssues, useImportPreview, type ImportPeople, type ImportPreview, type ImportReport } from "@/api/filters";
import { Button, Card, ErrorBanner, Page, PageHeader } from "@/components/ui";
import { Icon } from "@/components/icons";
import { MappingCard } from "@/features/import/MappingCard";
import { PeopleCard } from "@/features/import/PeopleCard";
import { WordsCard } from "@/features/import/WordsCard";

export { TARGET_LABELS } from "@/features/import/targets";

/** A CSV file becomes issues: choose it, map it, say what its words mean, try it dry. */
export const importRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/import",
  component: ImportPage,
});

function ImportPage() {
  const { projectKey } = importRoute.useParams();
  const { data } = useProject(projectKey);
  const preview = useImportPreview(projectKey);
  const run = useImportIssues(projectKey);
  const fileInput = useRef<HTMLInputElement>(null);
  const [file, setFile] = useState<File | null>(null);
  const [shape, setShape] = useState<ImportPreview | null>(null);
  const [mapping, setMapping] = useState<Record<string, number[]>>({});
  const [values, setValues] = useState<Record<string, Record<string, string>>>({});
  const [people, setPeople] = useState<ImportPeople>({ choices: {} });
  const [report, setReport] = useState<ImportReport | null>(null);

  function chosen(f: File | undefined) {
    if (!f) return;
    setFile(f);
    setReport(null);
    setValues({});
    setPeople({ choices: {} });
    preview.mutate(
      { file: f },
      {
        onSuccess: (p) => {
          setShape(p);
          setMapping(p.mapping);
        },
      },
    );
  }

  // The words and the people follow the mapping, so the file is read again
  // whenever it changes: it is the only way to know what is in those columns.
  function remap(next: Record<string, number[]>) {
    setMapping(next);
    setReport(null);
    if (file) preview.mutate({ file, mapping: next }, { onSuccess: setShape });
  }

  function go(dryRun: boolean) {
    if (!file) return;
    run.mutate({ file, decisions: { mapping, values, people }, dryRun }, { onSuccess: (r) => setReport(r.report) });
  }

  const ready = (mapping.summary?.length ?? 0) > 0;
  const made = report ? report.imported.length + report.updated.length : 0;
  return (
    <Page width="content">
      <PageHeader
        crumb={
          <Link to="/projects/$projectKey" params={{ projectKey }} className="hover:text-ink">
            {data?.project?.name ?? projectKey}
          </Link>
        }
        title="Import issues"
        meta="A CSV file, one issue per row: choose it, say which column is which and what its words mean here, try it dry, then import"
      />

      <Card className="mb-4 p-5" data-guide="import">
        <div className="flex flex-wrap items-center gap-3">
          <Button variant="secondary" icon={<Icon.Upload />} onClick={() => fileInput.current?.click()} loading={preview.isPending} data-action="choose-file">
            Choose a CSV file
          </Button>
          <input ref={fileInput} type="file" accept=".csv,text/csv" className="hidden" aria-label="Choose a CSV file" data-import-input onChange={(e) => chosen(e.target.files?.[0])} />
          {file && (
            <span className="text-sm text-ink-muted" data-import-file>
              {file.name}
              {shape ? `, ${shape.preview.total} row${shape.preview.total === 1 ? "" : "s"}, ${shape.preview.columns.length} columns` : ""}
            </span>
          )}
        </div>
        {preview.error && (
          <div className="mt-3">
            <ErrorBanner>{(preview.error as Error).message}</ErrorBanner>
          </div>
        )}
      </Card>

      {shape && (
        <>
          <MappingCard columns={shape.preview.columns} targets={shape.targets} mapping={mapping} onChange={remap} />
          <WordsCard words={shape.preview.words} values={values} onChange={setValues} />
          <PeopleCard people={shape.preview.people} choice={people} onChange={setPeople} />

          <Card className="mb-4 p-5">
            <div className="flex items-center gap-2">
              <Button variant="secondary" onClick={() => go(true)} loading={run.isPending} disabled={!ready} data-action="dry-run">
                Try it dry
              </Button>
              <Button onClick={() => go(false)} loading={run.isPending} disabled={!ready || !report?.dryRun} data-action="import" data-guide="import-go">
                Import {shape.preview.total} row{shape.preview.total === 1 ? "" : "s"}
              </Button>
              {!ready && <span className="text-sm text-ink-subtle">Map a column to the summary first.</span>}
              {ready && !report?.dryRun && <span className="text-sm text-ink-subtle">Try it dry before importing.</span>}
            </div>
            {run.error && (
              <div className="mt-3">
                <ErrorBanner>{(run.error as Error).message}</ErrorBanner>
              </div>
            )}
          </Card>
        </>
      )}

      {report && (
        <Card className="p-5" data-import-report={report.dryRun ? "dry" : "done"}>
          <h2 className="mb-2 text-sm font-medium text-ink">
            {report.dryRun ? `Dry run: ${made} of ${report.rows} rows would become issues` : `${made} of ${report.rows} rows became issues`}
          </h2>
          {report.updated.length > 0 && (
            <p className="mb-2 text-sm text-ink-muted" data-import-updated>
              {report.updated.length} were already here from an earlier run and were corrected.
            </p>
          )}
          {report.refused.length > 0 && (
            <ul className="space-y-1 text-sm">
              {report.refused.map((r) => (
                <li key={r.row} data-import-refusal={r.row}>
                  <span className="text-ink">Row {r.row}</span> <span className="text-ink-muted">{r.reason}</span>
                </li>
              ))}
            </ul>
          )}
          {report.partial.length > 0 && (
            <ul className="mt-2 space-y-1 text-sm">
              {report.partial.map((r) => (
                <li key={`partial-${r.row}`} data-import-partial={r.row} className="text-ink-muted">
                  {r.reason}
                </li>
              ))}
            </ul>
          )}
          {report.notes.length > 0 && (
            <ul className="mt-2 space-y-1 text-sm text-ink-muted" data-import-notes>
              {report.notes.map((note) => (
                <li key={note}>{note}</li>
              ))}
            </ul>
          )}
          {!report.dryRun && report.imported.length > 0 && (
            <p className="mt-2 text-sm text-ink-muted">
              Made: <span className="font-mono text-ink">{report.imported.join(", ")}</span>
            </p>
          )}
        </Card>
      )}
    </Page>
  );
}
