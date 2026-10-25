import type { settingsEn } from "./settings.en";

/** Die Texte der Einstellungsseiten; zuerst das eigene Formular des Profils. */
export const settingsDe: typeof settingsEn = {
  crumb: "Einstellungen",
  profile: {
    title: "Profil",
    saved: "Profil gespeichert",
    uploadPicture: "Bild hochladen",
    remove: "Entfernen",
    choosePicture: "Ein Bild wählen",
    pictureChanged: "Bild geändert",
    pictureRemoved: "Bild entfernt",
    pictureHint: "PNG oder JPEG, bis 2 MB. Erscheint überall, wo Ihre Initialen standen.",
    pictureTooBig: "Dieses Bild ist größer als 2 MB. Verkleinern Sie es und versuchen Sie es erneut.",
    name: "Name",
    email: "E-Mail",
    emailHint: "meldet Sie an und erhält die Post; sie wird hier nicht geändert.",
    timezone: "Zeitzone",
    language: "Sprache",
    languageHint: "Ändert die Sprache der Oberfläche, Englisch oder Deutsch, und wie Daten, Uhrzeiten und Zahlen geschrieben werden.",
    followBrowser: (locale: string) => `Wie im Browser (${locale})`,
    preview: "Daten werden so angezeigt:",
    save: "Änderungen speichern",
  },
};
