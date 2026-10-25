import { useState, type FormEvent } from "react";
import { useAddCompanyMember, useCompanies, useCreateCompany, useDeleteCompany, useRemoveCompanyMember, useUpdateCompany, type Company } from "@/api/desk";
import { Button, Card, ErrorBanner, Field, IconButton, SectionTitle, Tag, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { cleanDomain } from "./DeskExtras";
import { parseDomains } from "./company";
import { t } from "@/i18n";

// A desk's companies: customers who read the requests a colleague shared with
// them. Agents read the list; the desk's administrators keep it.
export function Companies({ projectKey, editable }: { projectKey: string; editable: boolean }) {
  const { data } = useCompanies(projectKey);
  const create = useCreateCompany(projectKey);
  const [name, setName] = useState("");
  const [domains, setDomains] = useState("");
  const companies = data?.companies ?? [];

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    create.mutate(
      { name: name.trim(), domains: parseDomains(domains) },
      {
        onSuccess: () => {
          setName("");
          setDomains("");
        },
      },
    );
  }

  return (
    <section data-guide="desk-companies">
      <SectionTitle className="mb-2">{t.desk.companies.title}</SectionTitle>
      <p className="mb-3 text-sm text-ink-muted">{t.desk.companies.intro}</p>
      {companies.length === 0 && !editable && <p className="text-sm text-ink-subtle">{t.desk.companies.none}</p>}
      <div className="space-y-3">
        {companies.map((c) => (
          <CompanyCard key={c.id} company={c} editable={editable} />
        ))}
      </div>
      {editable && (
        <Card className="mt-3 p-4">
          <form onSubmit={onSubmit} className="space-y-3" noValidate data-testid="new-company">
            {create.error && <ErrorBanner>{(create.error as Error).message}</ErrorBanner>}
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label={t.desk.companies.company} value={name} onChange={(e) => setName(e.target.value)} placeholder="Globex" />
              <Field
                label={t.desk.companies.domains}
                value={domains}
                onChange={(e) => setDomains(e.target.value)}
                placeholder="globex.test"
                hint={t.desk.companies.domainsHint}
              />
            </div>
            <Button type="submit" loading={create.isPending} disabled={!name.trim()}>
              {t.desk.companies.add}
            </Button>
          </form>
        </Card>
      )}
    </section>
  );
}

function CompanyCard({ company, editable }: { company: Company; editable: boolean }) {
  const update = useUpdateCompany();
  const remove = useDeleteCompany();
  const add = useAddCompanyMember();
  const takeOut = useRemoveCompanyMember();
  const confirm = useConfirm();
  const toast = useToast();
  const [email, setEmail] = useState("");
  const [domain, setDomain] = useState("");
  const [renaming, setRenaming] = useState(false);
  const [name, setName] = useState(company.name);
  const error = update.error ?? remove.error ?? add.error ?? takeOut.error;

  function addMember(event: FormEvent) {
    event.preventDefault();
    if (!email.trim()) return;
    add.mutate({ id: company.id, email: email.trim() }, { onSuccess: () => setEmail("") });
  }

  function addDomain(event: FormEvent) {
    event.preventDefault();
    const added = cleanDomain(domain);
    if (!added) return;
    update.mutate({ id: company.id, domains: [...company.domains, added] }, { onSuccess: () => setDomain("") });
  }

  function rename(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    update.mutate({ id: company.id, name: name.trim() }, { onSuccess: () => setRenaming(false) });
  }

  return (
    <Card className="space-y-3 p-4" data-company={company.name}>
      <div className="flex items-center justify-between gap-2">
        {renaming ? (
          <form onSubmit={rename} className="flex flex-1 items-end gap-2" noValidate>
            <div className="flex-1">
              <Field
                label={t.desk.companies.company}
                id={`field-company-rename-${company.id}`}
                value={name}
                onChange={(e) => setName(e.target.value)}
                controlSize="sm"
              />
            </div>
            <Button type="submit" size="sm" loading={update.isPending} disabled={!name.trim()}>
              {t.desk.companies.save}
            </Button>
            <Button type="button" size="sm" variant="ghost" onClick={() => setRenaming(false)}>
              {t.kit.cancel}
            </Button>
          </form>
        ) : (
          <h3 className="text-sm font-medium text-ink">{company.name}</h3>
        )}
        {editable && !renaming && (
          <span className="flex gap-1">
            <IconButton icon={<Icon.Edit />} label={t.desk.companies.rename(company.name)} size="sm" onClick={() => setRenaming(true)} />
            <IconButton
              icon={<Icon.Trash />}
              label={t.desk.companies.remove(company.name)}
              size="sm"
              onClick={async () =>
                (await confirm({
                  noun: t.kit.confirm.things.company,
                  verb: t.kit.confirm.verbs.remove,
                  body: t.desk.companies.removeBody(company.name),
                })) && remove.mutate(company.id, { onSuccess: () => toast.success(t.desk.companies.removed(company.name)) })
              }
            />
          </span>
        )}
      </div>

      <div className="space-y-1">
        <p className="text-xs font-medium tracking-wide text-ink-subtle uppercase">{t.desk.companies.domains}</p>
        {company.domains.length === 0 ? (
          <p className="text-sm text-ink-subtle">{t.desk.companies.noDomains}</p>
        ) : (
          <ul className="flex flex-wrap gap-2">
            {company.domains.map((d) => (
              <li key={d}>
                <Tag className="gap-1 py-0.5 text-xs" data-company-domain={d}>
                  {d}
                  {editable && (
                    <IconButton
                      icon={<Icon.X />}
                      label={t.desk.companies.takeDomainOff(d, company.name)}
                      size="xs"
                      disabled={update.isPending}
                      onClick={() => update.mutate({ id: company.id, domains: company.domains.filter((x) => x !== d) })}
                    />
                  )}
                </Tag>
              </li>
            ))}
          </ul>
        )}
        {editable && (
          <form onSubmit={addDomain} className="flex items-end gap-2" noValidate>
            <div className="flex-1">
              <Field
                label={t.desk.companies.domainFor(company.name)}
                id={`field-company-domain-${company.id}`}
                value={domain}
                onChange={(e) => setDomain(e.target.value)}
                placeholder="globex.test"
                controlSize="sm"
              />
            </div>
            <Button type="submit" size="sm" variant="secondary" disabled={!cleanDomain(domain) || update.isPending}>
              {t.desk.companies.addDomain}
            </Button>
          </form>
        )}
      </div>

      <div className="space-y-1">
        <p className="text-xs font-medium tracking-wide text-ink-subtle uppercase">{t.desk.companies.customers}</p>
        {company.members.length === 0 ? (
          <p className="text-sm text-ink-subtle">{t.desk.companies.nobody}</p>
        ) : (
          <ul className="divide-y divide-border rounded-md border border-border">
            {company.members.map((m) => (
              <li key={m.userId} className="flex items-center gap-2 px-3 py-1.5 text-sm" data-company-member={m.email}>
                <span className="min-w-0 flex-1 truncate text-ink">
                  {m.name} <span className="text-ink-muted">{m.email}</span>
                </span>
                {editable && (
                  <Button
                    variant="link"
                    className="text-xs text-ink-subtle hover:text-danger"
                    onClick={() => takeOut.mutate({ id: company.id, userId: m.userId })}
                  >
                    {t.desk.companies.takeOut}
                  </Button>
                )}
              </li>
            ))}
          </ul>
        )}
        {editable && (
          <form onSubmit={addMember} className="flex items-end gap-2" noValidate>
            <div className="flex-1">
              <Field
                label={t.desk.companies.customerFor(company.name)}
                id={`field-company-member-${company.id}`}
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                placeholder="name@globex.test"
                controlSize="sm"
              />
            </div>
            <Button type="submit" size="sm" variant="secondary" loading={add.isPending} disabled={!email.trim()}>
              {t.desk.companies.addCustomer}
            </Button>
          </form>
        )}
      </div>
      {error && <ErrorBanner>{(error as Error).message}</ErrorBanner>}
    </Card>
  );
}
