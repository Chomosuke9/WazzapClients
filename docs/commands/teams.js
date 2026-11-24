/// <reference path="wazzap.d.ts" />
// Splits the group into teams, and asks before sending them.
command({
  name: "teams",
  description: "Splits the group into random teams",
  options: [{ type: "number", name: "count", description: "How many teams", required: true, min: 2, max: 10 }],
  group: true,

  async run(ctx) {
    const people = (await ctx.members()).filter((m) => !m.me);
    const { count } = ctx.args;
    if (people.length < count) {
      await ctx.note(`There are only ${people.length} people to split.`);
      return;
    }
    while (true) {
      const teams = split(people, count);
      const shown = teams.map((team, i) => `Team ${i + 1}: ${team.map((m) => m.name).join(", ")}`);
      const answer = await ctx.ask(shown.join("\n"), ["Send", "Shuffle again"]);
      if (answer === null) return;
      if (answer === "Send") {
        await ctx.send(teams.map((team, i) => `*Team ${i + 1}:* ${team.map((m) => m.mention).join(" ")}`).join("\n"));
        return;
      }
    }
  },
});

function split(people, count) {
  const left = [...people];
  const teams = Array.from({ length: count }, () => []);
  for (let i = 0; left.length > 0; i++) {
    const [m] = left.splice(Math.floor(Math.random() * left.length), 1);
    teams[i % count].push(m);
  }
  return teams;
}
