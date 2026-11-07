// Exercise the production currency functions with ordinary decimal values.
const fs = require('fs');
const vm = require('vm');
const src = fs.readFileSync('dash/page/app.js', 'utf8');
const start = src.indexOf('function cents(s)');
const end = src.indexOf('// the sprint\'s duration', start);
if (start < 0 || end < 0) throw Error('money functions missing');
const context = {};
vm.runInNewContext(src.slice(start, end), context);
for (const [input, expected] of [['$26.16', 2616n], ['$300,000,000.01', 30000000001n], ['$90,071,992,547,409.93', 9007199254740993n], ['$0.00', 0n], ['-', null]]) {
  const actual = context.cents(input);
  if (actual !== expected) throw Error(`${input}: ${actual}, expected ${expected}`);
  if (actual !== null && context.cents(context.money(actual)) !== actual) throw Error(`round trip ${input}`);
}
if (context.money(context.cents('$90,071,992,547,409.93') + context.cents('$0.09')) !== '$90,071,992,547,410.02') throw Error('sum');
console.log('money checks passed');
