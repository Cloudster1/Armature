import type { guideEn } from "./guide.en";

/** Die Texte des Wegweisers, der sagt, wo etwas ist, und der Antworten des Assistenten. */
export const guideDe: typeof guideEn = {
  projectPages: {
    "": "Jeder Vorgang des Projekts als Tabelle, mit Filtern und einer Abfrage.",
    "/board": "Die Boards des Projekts: Spalten, Swimlanes und Karten, die Sie zwischen Status ziehen.",
    "/sprints": "Geplante, laufende und abgeschlossene Sprints, mit dem, was jeder enthielt.",
    "/plan": "Das Projekt als Zeitleiste: Balken, Abhängigkeiten und was zu spät ist.",
    "/calendar":
      "Ein Monat der datierten Arbeit des Projekts, mit Sprints, Meilensteinen und Versionen; ziehen Sie einen Vorgang, um seine Daten zu verschieben.",
    "/resources": "Die Arbeit je Team oder Person und Woche in Stunden, gegen die Stunden, die nach Feiertagen und Abwesenheiten bleiben.",
    "/milestones": "Meilensteine mit ihren Fälligkeitstagen und wie weit jeder ist.",
    "/dashboard": "Dashboards aus Widgets und Diagrammen über die eigenen Zahlen des Projekts.",
    "/hierarchy": "Das Projekt als Baum: Epics, ihre Untervorgänge und wie weit jeder ist.",
    "/queues": "Die Warteschlangen des Servicedesks mit Anfragen, die auf einen Bearbeiter warten.",
    "/service-desk": "Wie Anfragen zum Servicedesk kommen: Anfragetypen, das Portal und das Postfach.",
    "/teams": "Die Teams des Projekts, ihre Mitglieder und ihre Kapazität.",
    "/workflows": "Welchem Workflow jeder Vorgangstyp hier folgt, als Graph gezeichnet.",
    "/fields": "Die eigenen Felder, die dieses Projekt über die Standardspalten hinaus erfasst.",
    "/repositories": "Die mit dem Projekt verbundenen Git-Repositorys und ihre Webhooks.",
    "/releases": "Was das Projekt ausliefert: jede Version, wie nah sie ist, veröffentlicht oder nicht, und ihre Versionshinweise mit der erledigten Arbeit.",
    "/components": "Die Teile des Projekts, jedes mit einer Leitung und, wenn angegeben, wem neue Arbeit darin zugeht.",
    "/import": "Aus einer CSV-Datei werden Vorgänge: wählen Sie sie, ordnen Sie ihre Spalten zu, probieren Sie es trocken, dann importieren Sie.",
    "/automation": "Die Regeln des Projekts: wenn etwas geschieht, prüfen und handeln; jede Regel führt ein Protokoll ihrer Läufe.",
    "/settings": "Der Name des Projekts, seine Leitung und seine Archivierung.",
  },
  projectPageFallback: (label: string) => `Die Seite ${label} des Projekts.`,
  entries: {
    "page:home": { title: "Start", sentence: "Was in allen Projekten Ihres ist: Ihnen zugewiesen, beobachtet, zuletzt." },
    "page:projects": { title: "Projekte", sentence: "Jedes Projekt, das Sie sehen können, und wo ein neues aus einer Vorlage beginnt." },
    "page:search": {
      title: "Suche",
      sentence: "Durchsuchen Sie alle Projekte mit einer Abfrage; die Hilfe-Schaltfläche sagt, was eine Abfrage sagen kann.",
    },
    "page:filters": {
      title: "Gespeicherte Suchen",
      sentence: "Jede gespeicherte Suche, die Sie sehen dürfen: Ihre, geteilte, markierte; jede kann Ihnen täglich oder wöchentlich per E-Mail kommen.",
    },
    "page:inbox": {
      title: "Benachrichtigungen",
      sentence:
        "Worüber Sie informiert wurden: Ihnen zugewiesene Arbeit, Erwähnungen, Bewegungen an beobachteten Vorgängen; die Glocke in der Leiste zählt das Ungelesene und öffnet das Neueste.",
    },
    "page:settings": { title: "Einstellungen", sentence: "Ihre: Profil, Benachrichtigungseinstellungen, Designs und Token." },
    "page:admin": { title: "Admin", sentence: "Die aller: Installation, Zugriff, Benutzer, Workflows, Schlagwörter, Felder, Webhooks und das Prüfprotokoll." },
    "page:profile": {
      title: "Ihr Profil",
      sentence: "Ihr Name, Ihr Bild, Ihre Zeitzone und wie Daten für Sie geschrieben werden; das Menü des Avatars unten in der Leiste öffnet es.",
    },
    "page:tokens": {
      title: "Zugriffstoken",
      sentence: "Token für Skripte, CI-Jobs und Assistenten; eines kann nur lesen und nie schreiben.",
    },
    "page:installation": {
      title: "Installation",
      sentence: "Wie alle die Installation genannt sehen, oben in der Seitenleiste; Administratoren benennen sie hier um.",
    },
    "page:access": {
      title: "Zugriff",
      sentence: "Wer welche Rolle hat, die Gruppen, Einladungen und der Identitätsanbieter.",
    },
    "page:org-workflows": {
      title: "Workflows",
      sentence: "Die Workflows, denen jedes Projekt folgt, und die Schemata, die sie den Vorgangstypen zuweisen.",
    },
    "page:org-automation": {
      title: "Automatisierung über alle Projekte",
      sentence: "Regeln, die alle Projekte beobachten: wenn etwas geschieht, prüfen und handeln.",
    },
    "page:webhooks": {
      title: "Webhooks",
      sentence: "Wohin Ereignisse gesendet werden, signiert mit einem einmal gezeigten Geheimnis, mit einem Protokoll jedes Versuchs und einer Wiederholung.",
    },
    "page:holidays": {
      title: "Feiertage",
      sentence:
        "Kalender freier Tage, aus einer .ics-Datei importiert oder eingetippt; alle behalten den Standard, bis ihnen unter Benutzer ein anderer gegeben wird.",
    },
    "page:labels": { title: "Schlagwörter", sentence: "Wörter, die alle Projekte teilen, mit ihrer Häufigkeit." },
    "action:new-issue": {
      title: "Einen Vorgang anlegen",
      sentence: "Die Schaltfläche Neuer Vorgang in der Leiste legt einen im aktuellen Projekt an; Strg+K erreicht sie auch.",
    },
    "action:share-dashboard": {
      title: "Ein Dashboard teilen",
      sentence: "Teilen auf einem Dashboard erzeugt einen Link, der es ohne Anmeldung öffnet, bis Sie ihn widerrufen.",
    },
    "action:export-pdf": {
      title: "Ein Dashboard als PDF exportieren",
      sentence: "Als PDF exportieren auf einem Dashboard gibt eine Datei zurück, gedruckt von der geteilten Seite.",
    },
    "action:dashboard-template": {
      title: "Ein Dashboard aus einer Vorlage beginnen",
      sentence: "Neues Dashboard bietet die mitgelieferten und die für alle gespeicherten Vorlagen an; beim Anordnen gibt es Als Vorlage speichern.",
    },
    "action:milestone-dashboard": {
      title: "Ein Dashboard für einen Meilenstein",
      sentence: "Dashboard auf der Karte eines Meilensteins erzeugt eines über diesen Meilenstein aus einer Vorlage.",
    },
    "action:filter-dashboard": {
      title: "Ein Dashboard filtern",
      sentence: "Eine Filterkachel grenzt jedes Widget auf ein Team, einen Zeitraum, Vorgangstypen oder eine Abfrage ein.",
    },
    "action:map-workflow": {
      title: "Einem Vorgangstyp einen eigenen Workflow geben",
      sentence: "Auf der Workflow-Seite des Projekts entscheidet eine Projektverwaltung, welchem Workflow jeder Typ folgt.",
    },
    "action:design-workflow": {
      title: "Einen Workflow entwerfen",
      sentence: "Der Designer zeichnet Status und Übergänge auf eine Fläche; die Seite Workflows in der Administration öffnet ihn.",
    },
    "action:create-token": {
      title: "Ein Zugriffstoken erstellen",
      sentence: "Benennen Sie es auf der Seite Zugriffstoken; kreuzen Sie Darf nur lesen an für ein Token, das nichts ändert.",
    },
    "action:notification-settings": {
      title: "Wählen, wie Sie informiert werden",
      sentence:
        "Das Raster Benachrichtigungen in Ihrem Profil schaltet jeden Anlass für Benachrichtigungen hier und per E-Mail ein oder aus und bündelt E-Mails stündlich oder täglich.",
    },
    "action:mention": {
      title: "Jemanden in einem Kommentar erwähnen",
      sentence: "Tippen Sie ein At-Zeichen und einen Namen in einen Kommentar; die Person wird Beobachter und erfährt es.",
    },
    "action:automation-rule": {
      title: "Eine Regel anlegen",
      sentence:
        "Neue Regel auf der Seite Automatisierung des Projekts: ein Auslöser, Bedingungen und was zu tun ist; das Laufprotokoll sagt, warum jeder Lauf tat, was er tat.",
    },
    "action:save-search": {
      title: "Eine Suche speichern",
      sentence:
        "Diese Suche speichern unter der Abfrage auf der Suchseite benennt sie; eine gespeicherte Suche kann geteilt, markiert und per E-Mail gesendet werden.",
    },
    "action:export-csv": {
      title: "Vorgänge als CSV exportieren",
      sentence:
        "Exportieren auf der Suchseite schreibt die Vorgänge, auf die eine Abfrage passt, mit den angekreuzten Spalten in eine Datei; die Schaltfläche Mehr eines Vorgangs exportiert diesen Vorgang, angekreuzte Zeilen exportieren über die Leiste darüber, und die Liste eines Projekts exportiert, was sie zeigt.",
    },
    "action:bulk": {
      title: "Viele Vorgänge auf einmal ändern",
      sentence:
        "Kreuzen Sie Zeilen auf der Suchseite oder in der Vorgangsliste an, und die Leiste darüber ändert alle; was nicht geändert werden konnte, wird mit Grund aufgeführt.",
    },
    "action:clone": {
      title: "Einen Vorgang klonen",
      sentence: "Die Schaltfläche Mehr im Kopf eines Vorgangs kopiert ihn, auf Wunsch mit seinen Verknüpfungen und Unteraufgaben.",
    },
    "action:move": {
      title: "Arbeit woandershin verschieben",
      sentence: "Die Schaltfläche Mehr im Kopf eines Vorgangs verschiebt ihn; er behält seinen Verlauf, und seine alte Adresse findet ihn weiterhin.",
    },
    "action:knowledge-base": {
      title: "Eine Antwort einmal aufschreiben",
      sentence: "Artikel auf der Servicedesk-Seite werden einem Kunden angeboten, bevor er eine Anfrage stellt; veröffentlichen Sie einen, wenn er fertig ist.",
    },
    "action:canned": {
      title: "Mit einer Textvorlage antworten",
      sentence:
        "Auf der Servicedesk-Seite gespeicherte Antworten werden im Antwortfeld einer Anfrage angeboten, mit dem Namen des Kunden und dem Schlüssel eingesetzt.",
    },
    "action:business-hours": {
      title: "Nur Geschäftszeiten zählen",
      sentence:
        "Der Kalender eines Servicedesks auf der Servicedesk-Seite legt seine Zeiten fest, die Zeitzone, den Feiertagskalender, dem er folgt, und seine eigenen freien Tage; jedes Ziel wählt, ob das Wochenende zählt.",
    },
    "action:desk-companies": {
      title: "Kollegen gegenseitig ihre Anfragen sehen lassen",
      sentence:
        "Firmen auf der Servicedesk-Seite gruppieren die Kunden eines Servicedesks; wer eine Anfrage stellt, kann sie mit seiner Firma teilen, deren Mitglieder sie lesen, antworten und anhängen und nie Notizen sehen.",
    },
    "action:desk-door": {
      title: "Leute ohne Code hereinlassen",
      sentence:
        "Der Eingang auf der Servicedesk-Seite hat die Adresse zum Teilen und einen Schalter für den Code per E-Mail; ausgeschaltet wird einer Adresse nur für diesen Servicedesk vertraut.",
    },
    "action:csat": {
      title: "Wie Kunden den Servicedesk bewertet haben",
      sentence:
        "Die Lösungsmail bittet um eine Bewertung; die Warteschlange zeigt sie neben jeder Anfrage, und ein CSAT-Widget auf dem Dashboard zählt sie zusammen.",
    },
    "action:audit": {
      title: "Wer was in Armature getan hat",
      sentence:
        "Das Prüfprotokoll in der Administration führt vergebene Rollen, geänderte Gruppen, angelegte oder archivierte Projekte, Token und Anmeldungen auf, ein Jahr lang.",
    },
    "action:org-field": {
      title: "Ein Feld, das jedes Projekt hat",
      sentence:
        "Felder in der Administration gibt es in jedem Projekt; ein eigenes Feld eines Projekts wird von seiner Seite Felder dorthin übernommen, und gleichnamige Felder werden zu einem.",
    },
    "action:status-update": {
      title: "Sagen, wie es dem Projekt geht",
      sentence:
        "Veröffentlichen Sie eine Meldung auf der Vorgangsseite des Projekts: im Plan, gefährdet oder nicht im Plan, mit einer Notiz; die Projektliste zeigt die neueste.",
    },
    "action:theme": {
      title: "Das Design wechseln",
      sentence: "Die Schaltfläche unten in der Leiste wechselt zwischen dem System folgen, hell und dunkel.",
    },
    "action:demo-project": {
      title: "Das Demoprojekt hinzufügen",
      sentence:
        "Die Seite Installation füllt die Installation mit einem erfundenen Werk: einer Fertigungslinie und ihrer Wartung, mit Vorgängen, Sprints und Boards zum Zeigen.",
    },
    "action:invite": {
      title: "Jemanden einladen",
      sentence: "Unter Zugriff werden Einladungen verschickt und Rollen vergeben.",
    },
    "action:query-help": {
      title: "Was eine Abfrage sagen kann",
      sentence: "Die Hilfe-Schaltfläche neben dem Abfragefeld führt die Felder, Operatoren und Funktionen einer Abfrage auf.",
    },
    "action:issue-beside": {
      title: "Einen Vorgang neben der Liste öffnen",
      sentence:
        "Ein Klick auf eine Zeile der Vorgangsseite oder eine Karte auf dem Board öffnet sie in einem Bereich daneben; die Pfeiltasten gehen die Liste durch.",
    },
    "action:palette": {
      title: "Alles mit Strg+K erreichen",
      sentence: "Die Schnellsuche springt über seinen Schlüssel zu einem Vorgang, durchsucht Zusammenfassungen und erreicht jede Seite und Aktion.",
    },
    "intent:blocked": { title: "Was blockiert ist", sentence: "Die Abhängigkeitsansicht des Plans zeichnet, was auf was wartet." },
    "intent:mine": { title: "Meine offenen Vorgänge", sentence: "Alles, was Ihnen zugewiesen und nicht erledigt ist." },
    "intent:reported": { title: "Von mir gemeldete Vorgänge", sentence: "Alles, was Sie angelegt haben, in jedem Zustand." },
    "intent:due-week": { title: "Diese Woche fällig", sentence: "Offene Vorgänge, die bis zum Ende dieser Woche fällig sind." },
    "intent:overdue": { title: "Überfällige Vorgänge", sentence: "Offene Vorgänge, deren Fälligkeitstag vorbei ist." },
    "intent:today": { title: "Heute geändert", sentence: "Vorgänge, die jemand seit Mitternacht angefasst hat." },
    "intent:unassigned": { title: "Nicht zugewiesene Vorgänge", sentence: "Offene Vorgänge, die niemand hält." },
    "intent:urgent": { title: "Höchste Priorität", sentence: "Offene Vorgänge in den zwei höchsten Prioritäten." },
    "intent:bugs": { title: "Offene Bugs", sentence: "Jeder Bug, der nicht erledigt ist." },
    "intent:in-progress": { title: "Gerade in Arbeit", sentence: "Woran gerade gearbeitet wird." },
    "intent:done-week": { title: "Diese Woche erledigt", sentence: "Was seit Wochenbeginn gelöst wurde." },
    "intent:no-estimate": { title: "Ohne Schätzung", sentence: "Offene Vorgänge, die niemand geschätzt hat." },
  },
  ask: {
    asking: "Wird gefragt...",
    goThere: "Dorthin gehen",
    done: "Erledigt",
    confirm: "Bestätigen",
  },
};
