import { useState, type FormEvent } from "react";
import { useCreateUser } from "@/api/users";
import { Button, Card, ErrorBanner, Field, Select, useToast } from "@/components/ui";

const EMPTY = { email: "", name: "", role: "member" as "admin" | "member", password: "" };

/** Makes an account on the spot. The password is typed once and told to the person another way. */
export function NewUserForm() {
  const create = useCreateUser();
  const toast = useToast();
  const [draft, setDraft] = useState(EMPTY);
  const ready = draft.email.trim() !== "" && draft.name.trim() !== "" && draft.password !== "";

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!ready) return;
    create.mutate(
      { email: draft.email.trim(), name: draft.name.trim(), role: draft.role, password: draft.password },
      {
        onSuccess: (made) => {
          toast.success(`Created ${made.user.name}`);
          setDraft(EMPTY);
        },
      },
    );
  }

  return (
    <Card className="p-4">
      <form onSubmit={onSubmit} className="space-y-3" noValidate data-new-user>
        {create.error && <ErrorBanner>{(create.error as Error).message}</ErrorBanner>}
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="Email" type="email" autoComplete="off" value={draft.email} onChange={(e) => setDraft({ ...draft, email: e.target.value })} required />
          <Field label="Name" autoComplete="off" value={draft.name} onChange={(e) => setDraft({ ...draft, name: e.target.value })} required />
          <Select label="Role" value={draft.role} onChange={(e) => setDraft({ ...draft, role: e.target.value as "admin" | "member" })}>
            <option value="member">Member</option>
            <option value="admin">Administrator</option>
          </Select>
          <Field
            label="Password"
            type="password"
            autoComplete="new-password"
            hint="At least 12 characters. Tell the person, and ask them to change it from their profile."
            value={draft.password}
            onChange={(e) => setDraft({ ...draft, password: e.target.value })}
            required
          />
        </div>
        <Button type="submit" loading={create.isPending} disabled={!ready}>
          Create user
        </Button>
      </form>
    </Card>
  );
}
