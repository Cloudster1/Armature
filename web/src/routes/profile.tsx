import { Fragment, useEffect, useRef, useState, type FormEvent } from "react";
import { Link, createRoute, useNavigate } from "@tanstack/react-router";
import { appRoute } from "./app";
import { LOCALES, MY_DATA_HREF, useEraseMe, useMe, useRemoveAvatar, useUpdateProfile, useUploadAvatar } from "@/api/auth";
import { NOTIFICATION_KINDS, useNotificationPreferences, useSaveNotificationPreferences, type NotificationPreferences } from "@/api/notifications";
import { Avatar, Button, ButtonLink, Card, Checkbox, ErrorBanner, Field, Page, PageHeader, SectionTitle, Select, Tag, useToast } from "@/components/ui";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { Icon } from "@/components/icons";
import { AVATAR_MAX_BYTES } from "@/config";
import { makeFormat } from "@/lib/format";

/** Also reachable as /me, which is where a person guesses their own page is. */
export const profileRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/profile",
  component: ProfilePage,
});

// The zones the browser knows, which is every zone the API knows too since
// both carry the same database; a select rather than a text field, so a typo
// cannot be saved.
const timezones: string[] = (() => {
  try {
    return (Intl as unknown as { supportedValuesOf: (key: string) => string[] }).supportedValuesOf("timeZone");
  } catch {
    return ["UTC"];
  }
})();

function ProfilePage() {
  const { data } = useMe();
  const update = useUpdateProfile();
  const uploadAvatar = useUploadAvatar();
  const removeAvatar = useRemoveAvatar();
  const toast = useToast();
  const fileInput = useRef<HTMLInputElement>(null);
  const user = data?.principal?.user;
  const [name, setName] = useState("");
  const [timezone, setTimezone] = useState("UTC");
  const [locale, setLocale] = useState("en-GB");
  const [tooBig, setTooBig] = useState(false);

  useEffect(() => {
    if (user) {
      setName(user.name);
      setTimezone(user.timezone);
      setLocale(user.locale);
    }
  }, [user]);

  if (!user) return null;
  const preview = makeFormat(locale, timezones.includes(timezone) ? timezone : "UTC");
  const now = new Date().toISOString();

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    update.mutate({ name: name.trim(), timezone, locale }, { onSuccess: () => toast.success("Profile saved") });
  }

  function onPicture(file: File | undefined) {
    if (!file) return;
    if (file.size > AVATAR_MAX_BYTES) {
      setTooBig(true);
      return;
    }
    setTooBig(false);
    uploadAvatar.mutate(file, { onSuccess: () => toast.success("Picture changed") });
  }

  return (
    <Page width="narrow">
      <PageHeader
        crumb={
          <Link to="/settings" className="hover:text-ink">
            Settings
          </Link>
        }
        title="Profile"
        meta={user.email}
      />

      <Card className="mb-6 p-5">
        <div className="flex items-center gap-5">
          <Avatar name={user.name} src={user.avatarUrl} size="lg" />
          <div className="space-y-2">
            <div className="flex flex-wrap gap-2">
              <Button variant="secondary" icon={<Icon.Upload />} loading={uploadAvatar.isPending} onClick={() => fileInput.current?.click()}>
                Upload picture
              </Button>
              {user.avatarUrl && (
                <Button variant="ghost" loading={removeAvatar.isPending} onClick={() => removeAvatar.mutate(undefined, { onSuccess: () => toast.success("Picture removed") })}>
                  Remove
                </Button>
              )}
              <input
                ref={fileInput}
                type="file"
                accept="image/png,image/jpeg"
                className="hidden"
                aria-label="Choose a picture"
                data-avatar-input
                onChange={(e) => onPicture(e.target.files?.[0])}
              />
            </div>
            <p className="text-sm text-ink-subtle">PNG or JPEG, up to 2 MB. Shown wherever your initials were.</p>
            {tooBig && <ErrorBanner>That picture is over 2 MB. Make it smaller and try again.</ErrorBanner>}
            {uploadAvatar.error && <ErrorBanner>{(uploadAvatar.error as Error).message}</ErrorBanner>}
            {removeAvatar.error && <ErrorBanner>{(removeAvatar.error as Error).message}</ErrorBanner>}
          </div>
        </div>
      </Card>

      <Card className="p-5">
        <form onSubmit={onSubmit} className="space-y-4" noValidate>
          {update.error && <ErrorBanner>{(update.error as Error).message}</ErrorBanner>}
          <Field label="Name" required value={name} onChange={(e) => setName(e.target.value)} />
          <div className="space-y-1">
            <span className="block text-sm font-medium text-ink-muted">Email</span>
            <p className="text-sm text-ink">
              {user.email} <span className="text-ink-subtle">signs you in and gets the mail; it does not change here.</span>
            </p>
          </div>
          <Select label="Time zone" value={timezone} onChange={(e) => setTimezone(e.target.value)}>
            {!timezones.includes(timezone) && <option value={timezone}>{timezone}</option>}
            {timezones.map((zone) => (
              <option key={zone} value={zone}>
                {zone.replace(/_/g, " ")}
              </option>
            ))}
          </Select>
          <Select label="Language" value={locale} onChange={(e) => setLocale(e.target.value)} hint="Changes how dates, times and numbers are written; the interface itself stays in English.">
            {/* Empty is what a new account has: the browser decides, and the select says so. */}
            <option value="">Same as the browser ({navigator.language})</option>
            {LOCALES.map((l) => (
              <option key={l.code} value={l.code}>
                {l.label}
              </option>
            ))}
          </Select>
          <p className="text-sm text-ink-muted" data-format-preview>
            Dates are shown as <span className="text-ink tabular-nums">{preview.dateTime(now)}</span>.
          </p>
          <Button type="submit" loading={update.isPending} disabled={!name.trim()}>
            Save changes
          </Button>
        </form>
      </Card>

      <NotificationSettings />

      <section className="mt-8">
        <SectionTitle className="mb-2">Organizations</SectionTitle>
        <Card className="divide-y divide-border">
          {(data?.organizations ?? []).map((org) => (
            <div key={org.orgId} className="flex items-center gap-3 px-4 py-2.5 text-sm" data-membership={org.orgSlug}>
              <span className="min-w-0 flex-1 truncate text-ink">{org.orgName}</span>
              <Tag className="capitalize">{org.role}</Tag>
            </div>
          ))}
        </Card>
      </section>

      <YourData name={user.name} />
    </Page>
  );
}

// What is held about a person is theirs to take and theirs to have erased.
// Erasing keeps what they wrote, attributed to "Former user"; the confirm
// says so, because it is the one thing people ask before pressing.
function YourData({ name }: { name: string }) {
  const erase = useEraseMe();
  const confirm = useConfirm();
  const navigate = useNavigate();
  return (
    <section className="mt-8">
      <SectionTitle className="mb-2">Your data</SectionTitle>
      <Card className="space-y-4 p-5">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <p className="text-sm text-ink-muted">Everything held about you, as one JSON file: your profile, where you belong, your sessions, and what you reported, wrote and watched.</p>
          <ButtonLink href={MY_DATA_HREF} download icon={<Icon.Download />} data-action="export-me">
            Download my data
          </ButtonLink>
        </div>
        <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border pt-4">
          <p className="text-sm text-ink-muted">Deleting your account ends every session and token and takes your name and address out. Issues and comments you wrote stay, by "Former user".</p>
          <Button
            variant="danger"
            loading={erase.isPending}
            data-action="erase-me"
            onClick={async () => {
              if (await confirm({ noun: "account", verb: "Delete", body: `${name}'s name, address, sessions and tokens go for good. What you wrote stays, by "Former user".` })) {
                erase.mutate(undefined, { onSuccess: () => navigate({ to: "/login" }) });
              }
            }}
          >
            Delete my account
          </Button>
        </div>
        {erase.error && <ErrorBanner>{(erase.error as Error).message}</ErrorBanner>}
      </Card>
    </section>
  );
}

// How this person is told: each reason in the inbox, by mail, or neither, and
// whether mail comes at once or bundled. Unset means on, so the grid reads
// the maps the way the API does.
function NotificationSettings() {
  const { data } = useNotificationPreferences();
  const save = useSaveNotificationPreferences();
  const toast = useToast();
  const [draft, setDraft] = useState<NotificationPreferences | null>(null);
  const prefs = draft ?? data?.preferences;
  if (!prefs) return null;

  const on = (map: Record<string, boolean>, kind: string) => map[kind] !== false;
  const set = (channel: "mail" | "inapp", kind: string, value: boolean) =>
    setDraft({ ...prefs, [channel]: { ...prefs[channel], [kind]: value } });

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!prefs) return;
    save.mutate(prefs, { onSuccess: () => { setDraft(null); toast.success("Notification settings saved"); } });
  }

  return (
    <section className="mt-8">
      <SectionTitle className="mb-2">Notifications</SectionTitle>
      <Card className="p-5">
        <form onSubmit={onSubmit} className="space-y-4" noValidate data-notification-settings data-guide="notification-settings">
          {save.error && <ErrorBanner>{(save.error as Error).message}</ErrorBanner>}
          <div className="grid grid-cols-[1fr_auto_auto] gap-x-6 gap-y-2 text-sm">
            <span className="text-ink-subtle">Tell me when</span>
            <span className="text-ink-subtle">Inbox</span>
            <span className="text-ink-subtle">Mail</span>
            {NOTIFICATION_KINDS.map(({ kind, label }) => (
              <Fragment key={kind}>
                <span className="text-ink">{label}</span>
                <Checkbox label={<span className="sr-only">{label} in the inbox</span>} checked={on(prefs.inapp, kind)} onChange={(e) => set("inapp", kind, e.target.checked)} data-pref-inapp={kind} />
                <Checkbox label={<span className="sr-only">{label} by mail</span>} checked={on(prefs.mail, kind)} onChange={(e) => set("mail", kind, e.target.checked)} data-pref-mail={kind} />
              </Fragment>
            ))}
          </div>
          <Select label="Mail arrives" value={prefs.digest} onChange={(e) => setDraft({ ...prefs, digest: e.target.value as NotificationPreferences["digest"] })} id="field-digest" hint="Bundled mail lists everything since the last one, in one message.">
            <option value="off">As it happens</option>
            <option value="hourly">Bundled, once an hour</option>
            <option value="daily">Bundled, once a day</option>
          </Select>
          <Button type="submit" loading={save.isPending} disabled={!draft} data-action="save-notifications">
            Save notification settings
          </Button>
        </form>
      </Card>
    </section>
  );
}
