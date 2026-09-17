# Reports which tools each agent actually receives, and the approximate token
# cost of their descriptions. A tool is dropped from the request only when the
# last matching rule is `"*": deny` (permission/index.ts:204-213, applied at
# session/llm/request.ts:207). Models the real merge order:
# shared defaults -> per-agent built-in -> opencode.jsonc -> frontmatter.
# Run from the repo root: ruby archive/agent-tool-visibility.rb
require 'yaml'
def wm(i,p); Regexp.new("\\A"+p.gsub(/[.+^${}()|\[\]\\]/){|c| "\\"+c}.gsub('*','.*').gsub('?','.')+"\\z",Regexp::MULTILINE).match?(i); end
TOK = {"edit"=>342,"write"=>155,"todowrite"=>503,"task"=>576,"read"=>289,"grep"=>164,
       "glob"=>129,"bash"=>317,"lsp"=>325,"skill"=>99,"webfetch"=>187,"websearch"=>258,"question"=>164,
       "plan_enter"=>153,"plan_exit"=>144}
EDITS = %w[edit write apply_patch]  # apply_patch omitted from TOK: registry.ts sends it only to gpt-* models
# shared defaults (agent/agent.ts:119-135) then user global (opencode.jsonc) then frontmatter
DEFAULTS = [["*","*","allow"],["doom_loop","*","ask"],["question","*","deny"],["plan_enter","*","deny"],["plan_exit","*","deny"]]
# per-agent built-in layers, agent/agent.ts:140-215, merged between defaults and user
BUILTIN = {
 "build"=>[["question","*","allow"],["plan_enter","*","allow"]],
 "general"=>[["todowrite","*","deny"]],
 "explore"=>[["*","*","deny"],["grep","*","allow"],["glob","*","allow"],["list","*","allow"],
             ["bash","*","allow"],["webfetch","*","allow"],["websearch","*","allow"],["read","*","allow"]],
}
USER     = [["read","*","deny"],["lsp","*","deny"],["todowrite","*","allow"],["edit","*","allow"],["external_directory","*","allow"],["plan_enter","*","deny"],["plan_exit","*","deny"],["task","custom","ask"]]
def rules(fm)
  out=[]
  (fm["permission"]||{}).each{|k,v| v.is_a?(String) ? out<<[k,"*",v] : v.each{|pat,a| out<<[k,pat,a]}}
  out
end
puts format("%-14s %6s  %s","agent","~tok","hidden tools")
tot={}
Dir["agents/*.md"].sort.each do |f|
  n=File.basename(f,".md"); next if %w[compaction title summary].include?(n)
  fm=YAML.safe_load(File.read(f).split(/^---$/)[1])||{}
  rs = DEFAULTS + (BUILTIN[n]||[]) + USER + rules(fm)
  # subagent session-level denies (task.ts:143-155) for todowrite/task when no literal key
  if fm["mode"]=="subagent"
    
    rs << ["task","*","deny"]      unless rules(fm).any?{|p,_,_| p=="task"}
  end
  hidden = TOK.keys.select do |t|
    perm = EDITS.include?(t) ? "edit" : t
    r = rs.select{|p,_,_| wm(perm,p)}.last
    r && r[1]=="*" && r[2]=="deny"
  end
  vis = TOK.reject{|k,_| hidden.include?(k)}.values.sum
  tot[n]=vis
  puts format("%-14s %6d  %s", n, vis, hidden.sort.join(" "))
end
