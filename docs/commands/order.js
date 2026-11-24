/// <reference path="wazzap.d.ts" />
// A template: the options are filled into the text, which is sent.
command({
  name: "order",
  description: "Sends an order",
  options: [
    { type: "number", name: "qty", description: "How many", required: true, min: 1, max: 99 },
    { type: "text", name: "item", description: "What to order", required: true },
  ],
  text: "*Order*\n{qty} × {item}\n_{date}, {time}_",
});
