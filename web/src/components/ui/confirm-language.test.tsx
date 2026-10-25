import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import * as i18n from "@/i18n";
import { ConfirmDialog } from "./index";

afterEach(() => {
  cleanup();
  i18n.applyLanguage("");
});

function ask() {
  render(
    <ConfirmDialog open onClose={() => {}} onConfirm={() => {}} noun={i18n.t.kit.confirm.things.comment} verb={i18n.t.kit.confirm.verbs.delete} body="Body" />,
  );
}

// The verb and the thing come in the order the language says them.
describe("the confirm dialog's words", () => {
  it("asks in English verb first", () => {
    ask();
    expect(screen.getByRole("dialog", { name: "Delete comment?" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Delete comment" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeInTheDocument();
  });

  it("asks in German with the thing first", () => {
    i18n.applyLanguage("de");
    ask();
    expect(screen.getByRole("dialog", { name: "Kommentar löschen?" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Kommentar löschen" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Abbrechen" })).toBeInTheDocument();
  });
});
