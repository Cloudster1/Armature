/** The settings pages' strings; the profile's own form first, the other pages as they move here. */
export const settingsEn = {
  crumb: "Settings",
  profile: {
    title: "Profile",
    saved: "Profile saved",
    uploadPicture: "Upload picture",
    remove: "Remove",
    choosePicture: "Choose a picture",
    pictureChanged: "Picture changed",
    pictureRemoved: "Picture removed",
    pictureHint: "PNG or JPEG, up to 2 MB. Shown wherever your initials were.",
    pictureTooBig: "That picture is over 2 MB. Make it smaller and try again.",
    name: "Name",
    email: "Email",
    emailHint: "signs you in and gets the mail; it does not change here.",
    timezone: "Time zone",
    language: "Language",
    languageHint: "Changes the language of the interface, English or German, and how dates, times and numbers are written.",
    followBrowser: (locale: string) => `Follow the browser (${locale})`,
    preview: "Dates are shown as",
    save: "Save changes",
  },
};
