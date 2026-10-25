import { Card, Input, Labelled, Table, Td, Th } from "@/components/ui";
import type { ImportPeople, ImportPerson } from "@/api/filters";
import { PersonPicker } from "@/features/people/PersonPicker";
import { t } from "@/i18n";

/** Who the file's names are here. Jira writes a username, not an address. */
export function PeopleCard({ people, choice, onChange }: { people: ImportPerson[]; choice: ImportPeople; onChange: (choice: ImportPeople) => void }) {
  if (people.length === 0) return null;

  const choices = choice.choices ?? {};
  const making = Object.values(choices).some((c) => c.create);

  function pick(name: string, value: string) {
    const next = { ...choices };
    if (value === "create") next[name] = { create: true };
    else if (value) next[name] = { member: value };
    else next[name] = {};
    onChange({ ...choice, choices: next });
  }

  function valueFor(person: ImportPerson): string {
    const picked = choices[person.name];
    if (picked?.create) return "create";
    if (picked?.member) return picked.member;
    if (picked) return "";
    return person.member ?? "";
  }

  return (
    <Card className="mb-4 p-5" data-import-people>
      <h2 className="mb-1 text-sm font-medium text-ink">{t.projectSetup.import.people}</h2>
      <p className="mb-3 text-sm text-ink-muted">{t.projectSetup.import.peopleDetail}</p>
      <Table dense>
        <thead>
          <tr>
            <Th>{t.projectSetup.import.inFile}</Th>
            <Th>{t.projectSetup.import.rowCount}</Th>
            <Th>{t.projectSetup.import.is}</Th>
          </tr>
        </thead>
        <tbody>
          {people.map((person) => (
            <tr key={person.name} data-import-person={person.name}>
              <Td className="text-sm text-ink">{person.name}</Td>
              <Td className="text-sm text-ink-muted">{person.count}</Td>
              <Td>
                {/* Each name searches on its own, so a file of fifty names never lists everybody fifty times. */}
                <PersonPicker
                  id={`person-${person.name}`}
                  aria-label={t.projectSetup.import.who(person.name)}
                  controlSize="sm"
                  none={t.projectSetup.import.leaveEmpty}
                  optionLabel={(m) => `${m.name} (${m.email})`}
                  selected={person.member ? { id: person.member, name: person.email ?? person.name } : null}
                  value={valueFor(person)}
                  onChange={(value) => pick(person.name, value)}
                  data-import-person-choice={person.name}
                >
                  <option value="create">{t.projectSetup.import.makeAccount}</option>
                </PersonPicker>
              </Td>
            </tr>
          ))}
        </tbody>
      </Table>
      {making && (
        <div className="mt-4 max-w-xs">
          <Labelled id="import-domain" label={t.projectSetup.import.domain}>
            <Input
              id="import-domain"
              value={choice.domain ?? ""}
              placeholder="example.com"
              onChange={(e) => onChange({ ...choice, domain: e.target.value })}
              data-import-domain
            />
          </Labelled>
          <p className="mt-1 text-sm text-ink-subtle">{t.projectSetup.import.domainHint}</p>
        </div>
      )}
    </Card>
  );
}
