import { Card, Input, Labelled, Table, Td, Th } from "@/components/ui";
import type { ImportPeople, ImportPerson } from "@/api/filters";
import { PersonPicker } from "@/features/people/PersonPicker";

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
      <h2 className="mb-1 text-sm font-medium text-ink">Who these people are</h2>
      <p className="mb-3 text-sm text-ink-muted">A name nobody here answers to leaves the field empty, unless you give that person an account.</p>
      <Table dense>
        <thead>
          <tr>
            <Th>In the file</Th>
            <Th>On how many rows</Th>
            <Th>Is</Th>
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
                  aria-label={`Who ${person.name} is`}
                  controlSize="sm"
                  none="Leave it empty"
                  optionLabel={(m) => `${m.name} (${m.email})`}
                  selected={person.member ? { id: person.member, name: person.email ?? person.name } : null}
                  value={valueFor(person)}
                  onChange={(value) => pick(person.name, value)}
                  data-import-person-choice={person.name}
                >
                  <option value="create">Make an account for them</option>
                </PersonPicker>
              </Td>
            </tr>
          ))}
        </tbody>
      </Table>
      {making && (
        <div className="mt-4 max-w-xs">
          <Labelled id="import-domain" label="Addresses are made at">
            <Input id="import-domain" value={choice.domain ?? ""} placeholder="example.com" onChange={(e) => onChange({ ...choice, domain: e.target.value })} data-import-domain />
          </Labelled>
          <p className="mt-1 text-sm text-ink-subtle">An account made this way cannot sign in until somebody sets it up.</p>
        </div>
      )}
    </Card>
  );
}
