/** Words for things several areas name: a token's scopes, and what a code host calls a pull request. */
export const termsEn = {
  scopes: {
    read: "See the projects and issues you can see",
    "issue.write": "File and edit issues, and link pages to them",
    "issue.transition": "Move issues through their workflow",
    "comment.write": "Comment on issues",
    "git.write": "Push code to the branches of issues you may edit",
    "webhooks.partner": "Send Armature's events to it, from every project (administrators only)",
  } as Record<string, string>,
  pull: { github: "pull request", gitlab: "merge request" },
};
