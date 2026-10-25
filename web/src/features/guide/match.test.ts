import { describe, expect, it } from "vitest";
import { answer, looksLikeQuestion, words } from "./match";
import { CATALOGUE, type GuideEntry } from "./catalogue";

const inProject = { projectKey: "CP", canAdministerOrg: true };
const member = { projectKey: "CP", canAdministerOrg: false };
const nowhere = { canAdministerOrg: false };

describe("the guide", () => {
  it("finds the page a question names", () => {
    const [best] = answer("how do I share a dashboard?", inProject);
    expect(best?.entry.id).toBe("action:share-dashboard");
    expect(best?.entry.to(inProject)).toEqual({ to: "/projects/$projectKey/dashboard", params: { projectKey: "CP" } });
  });

  it("understands the reader's words for the product's", () => {
    const ids = answer("where are the tickets of this project", inProject).map((a) => a.entry.id);
    expect(ids[0]).toBe("page:/issues");
    expect(answer("gantt view", inProject)[0]?.entry.id).toBe("page:/plan");
    expect(answer("api key for a script", nowhere)[0]?.entry.id).toMatch(/token/);
    expect(answer("where are my notifications", nowhere)[0]?.entry.id).toBe("page:inbox");
    expect(answer("turn off the notification mails", nowhere)[0]?.entry.id).toBe("action:notification-settings");
  });

  it("turns a question about work into a query", () => {
    const [best] = answer("what is due this week", nowhere);
    expect(best?.entry.id).toBe("intent:due-week");
    expect(best?.entry.to(nowhere).search).toEqual({ q: "due <= endOfWeek() AND statusCategory != done ORDER BY due ASC" });
  });

  it("keeps an administrator's page from a member", () => {
    expect(answer("invite a colleague", inProject)[0]?.entry.id).toBe("action:invite");
    expect(answer("invite a colleague", member).some((a) => a.entry.needs === "admin")).toBe(false);
  });

  it("sends a project page to the projects list when no project is open", () => {
    const [best] = answer("open the board", nowhere);
    expect(best?.entry.needs).toBe("project");
  });

  it("says nothing rather than guessing", () => {
    expect(answer("purple monkey dishwasher", inProject)).toEqual([]);
    expect(answer("", inProject)).toEqual([]);
  });

  it("knows a question from a search", () => {
    expect(looksLikeQuestion("CP-12")).toBe(false);
    expect(looksLikeQuestion("login")).toBe(false);
    expect(looksLikeQuestion("share dashboard")).toBe(true);
    expect(looksLikeQuestion("theme?")).toBe(true);
  });

  it("drops the words that say nothing", () => {
    expect(words("Where do I find the issues?")).toEqual(["issue"]);
  });

  it("gives every entry a sentence and a way there", () => {
    const seen = new Set<string>();
    for (const entry of CATALOGUE as GuideEntry[]) {
      expect(seen.has(entry.id), entry.id).toBe(false);
      seen.add(entry.id);
      expect(entry.sentence.endsWith("."), entry.id).toBe(true);
      expect(entry.to(inProject).to.startsWith("/"), entry.id).toBe(true);
    }
  });
});

describe("the guide in German", () => {
  // Read through the module: t and the language are live, and a copy would keep English.
  async function inGerman<T>(fn: () => T): Promise<T> {
    const i18n = await import("@/i18n");
    i18n.applyLanguage("de");
    try {
      return fn();
    } finally {
      i18n.applyLanguage("");
    }
  }

  it("finds the page a German question names", async () => {
    const ids = await inGerman(() => ({
      plan: answer("Wo finde ich den Zeitplan?", inProject)[0]?.entry.id,
      token: answer("Wie erstelle ich ein Zugriffstoken?", nowhere)[0]?.entry.id,
      overdue: answer("Welche Vorgänge sind überfällig?", nowhere)[0]?.entry.id,
      invite: answer("Wie kann ich einen Kollegen einladen?", inProject)[0]?.entry.id,
    }));
    expect(ids).toEqual({ plan: "page:/plan", token: "action:create-token", overdue: "intent:overdue", invite: "action:invite" });
  });

  it("still understands an English word in a German interface", async () => {
    expect(await inGerman(() => answer("gantt", inProject)[0]?.entry.id)).toBe("page:/plan");
  });

  it("says its titles and sentences in German, and in English again after", async () => {
    const said = await inGerman(() => {
      const entry = CATALOGUE.find((e) => e.id === "action:share-dashboard")!;
      const plan = CATALOGUE.find((e) => e.id === "page:/plan")!;
      return { title: entry.title, sentence: entry.sentence, plan: plan.sentence };
    });
    expect(said.title).toBe("Ein Dashboard teilen");
    expect(said.sentence).toMatch(/^Teilen auf einem Dashboard/);
    expect(said.plan).toMatch(/^Das Projekt als Zeitleiste/);
    expect(CATALOGUE.find((e) => e.id === "action:share-dashboard")!.title).toBe("Share a dashboard");
  });

  it("gives every entry German words to be found by", async () => {
    const { KEYWORDS_DE } = await import("./words.de");
    for (const entry of CATALOGUE as GuideEntry[]) expect(KEYWORDS_DE[entry.id]?.length ?? 0, entry.id).toBeGreaterThan(0);
  });
});
