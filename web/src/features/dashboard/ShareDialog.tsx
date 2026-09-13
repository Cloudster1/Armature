import { useState, type FormEvent } from "react";
import { useCreateShare, useRevokeShare, useShares, type Share } from "@/api/shares";
import { Button, Dialog, ErrorBanner, Field, IconButton, Input, Table, Td, Th, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { useFormat } from "@/lib/format";
import { useConfirm } from "@/features/shell/ConfirmProvider";

/**
 * The links that open this dashboard without a sign-in. A new link's address
 * is shown once, here; afterwards the row knows only that it exists.
 */
export function ShareDialog({ dashboardId, dashboardName, query, open, onClose }: { dashboardId: string; dashboardName: string; query: string; open: boolean; onClose: () => void }) {
  const { data, error } = useShares(dashboardId);
  const create = useCreateShare();
  const revoke = useRevokeShare();
  const confirm = useConfirm();
  const toast = useToast();
  const format = useFormat();
  const [name, setName] = useState("");
  const [expiresOn, setExpiresOn] = useState("");
  const [made, setMade] = useState<{ share: Share; url: string } | null>(null);
  const shares = data?.shares ?? [];

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    create.mutate(
      { dashboardId, name: name.trim(), query, expiresAt: expiresOn ? `${expiresOn}T23:59:59Z` : undefined },
      {
        onSuccess: (answer) => {
          setMade(answer);
          setName("");
          setExpiresOn("");
        },
      },
    );
  }

  async function copy(url: string) {
    try {
      await navigator.clipboard.writeText(url);
      toast.success("Copied the link");
    } catch {
      toast.error("Copy the link from the field; the clipboard was not reachable.");
    }
  }

  return (
    <Dialog open={open} onClose={onClose} title={`Share ${dashboardName}`} size="lg" attrs={{ "data-share-dialog": "" }} description="A link shows the dashboard as it is filtered now, to anybody who has it, until you revoke it.">
      <div className="space-y-5">
        {made && (
          <div className="rounded-overlay border border-accent bg-accent-subtle p-3" data-share-made="">
            <p className="text-sm font-medium text-ink">{made.share.name} is ready. Copy the address now; it is not shown again.</p>
            <div className="mt-2 flex items-center gap-2">
              <Input readOnly value={made.url} aria-label="Link address" className="min-w-0 flex-1 font-mono text-xs" data-share-url={made.url} onFocus={(e) => e.currentTarget.select()} />
              <IconButton icon={<Icon.Copy />} label="Copy the link" size="md" onClick={() => copy(made.url)} data-action="copy-share" />
            </div>
          </div>
        )}

        <form onSubmit={onSubmit} className="flex flex-wrap items-end gap-2" noValidate>
          <Field label="Link name" value={name} onChange={(e) => setName(e.target.value)} placeholder="Lobby screen" className="w-56" />
          <Field label="Expires on" type="date" value={expiresOn} onChange={(e) => setExpiresOn(e.target.value)} className="w-44" hint="Optional" />
          <Button type="submit" loading={create.isPending} disabled={!name.trim()} icon={<Icon.Share />}>
            New link
          </Button>
        </form>
        {query && (
          <p className="text-xs text-ink-subtle">
            Frozen into a new link: <code className="font-mono text-ink-muted">{query}</code>
          </p>
        )}
        {create.error && <ErrorBanner>{(create.error as Error).message}</ErrorBanner>}
        {error && <ErrorBanner>{(error as Error).message}</ErrorBanner>}

        {shares.length > 0 ? (
          <Table dense>
            <thead>
              <tr>
                <Th>Link</Th>
                <Th>Made</Th>
                <Th>Expires</Th>
                <Th className="w-24" aria-label="Revoke" />
              </tr>
            </thead>
            <tbody>
              {shares.map((share) => (
                <tr key={share.id} data-share={share.name}>
                  <Td className="font-medium text-ink">{share.name}</Td>
                  <Td className="text-ink-muted">{format.date(share.createdAt)}</Td>
                  <Td className="text-ink-muted">{share.expiresAt ? format.date(share.expiresAt) : "Never"}</Td>
                  <Td className="text-right">
                    <Button
                      size="sm"
                      variant="ghost"
                      data-action="revoke-share"
                      onClick={async () => (await confirm({ noun: "link", verb: "Revoke", body: `${share.name} stops opening the dashboard. Anybody holding it sees that it is no longer valid.` })) && revoke.mutate({ dashboardId, shareId: share.id })}
                    >
                      Revoke
                    </Button>
                  </Td>
                </tr>
              ))}
            </tbody>
          </Table>
        ) : (
          <p className="text-sm text-ink-subtle">No links yet.</p>
        )}
      </div>
    </Dialog>
  );
}
