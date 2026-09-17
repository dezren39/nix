# Verifies the agents/*.md delegation graph is acyclic and reports the longest
# chain, so subagent_depth in opencode.jsonc can be checked against it.
# Reimplements util/wildcard.ts matching and permission/index.ts findLast order.
# Run from the repo root: ruby archive/agent-dag-check.rb
require 'yaml'
# Mirrors util/wildcard.ts:3-13 — escape regex metachars, * -> .*, ? -> ., anchor both ends
def wmatch(input, pattern)
  esc = pattern.gsub(/[.+^${}()|\[\]\\]/) { |c| "\\" + c }.gsub('*', '.*').gsub('?', '.')
  Regexp.new("\\A" + esc + "\\z", Regexp::MULTILINE).match?(input)
end
AGENTS = Dir['agents/*.md'].map { |f| [File.basename(f,'.md'), YAML.safe_load(File.read(f).split(/^---$/)[1]) || {}] }.to_h
names = AGENTS.keys
def evaluate(perm, target)
  rules = []
  (perm || {}).each do |k, v|
    v.is_a?(String) ? rules << [k, '*', v] : v.each { |pat, act| rules << [k, pat, act] }
  end
  m = rules.select { |p, pat, _| wmatch('task', p) && wmatch(target, pat) }.last
  m ? m[2] : 'ask(default)'
end
edges = {}
AGENTS.each do |name, fm|
  next if fm['mode'] == 'primary' && fm['hidden']
  edges[name] = names.select { |t| AGENTS[t]['mode'] != 'primary' && evaluate(fm['permission'], t) != 'deny' }
end
puts "EDGES"
edges.sort.each { |k, v| puts format("  %-14s -> %s", k, v.empty? ? "(none)" : v.sort.join(' ')) }
longest = lambda do |n, stack|
  return [0, []] unless edges[n]
  raise "CYCLE: #{(stack + [n]).join(' -> ')}" if stack.include?(n)
  best = [0, []]
  edges[n].each { |t| d, p = longest.call(t, stack + [n]); best = [d+1, [t]+p] if d+1 > best[0] }
  best
end
puts "\nLONGEST CHAINS"
edges.keys.sort.each { |n| d, p = longest.call(n, []); puts format("  %-14s depth %d  %s", n, d, ([n]+p).join(' -> ')) }
