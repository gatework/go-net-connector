# frozen_string_literal: true
# Run against frozen local sources; no device, DNS, inventory or credentials.
root = File.expand_path('..', __dir__)
%w[net-connector expect-ruby ctx_cli].each { |name| $LOAD_PATH.unshift(File.join(root, '.reference', name, 'lib')) }
require 'net/connector'
require 'net/connector/device/tftp/target'
require 'json'
profiles = Net::Connector.vendors.to_h do |vendor|
  profile = Net::Connector.vendor_class(vendor).profile
  [vendor, { config: profile.config_commands, save: profile.save_commands, privilege: profile.privilege_command, timeout: profile.command_timeout }]
end
context = Data.define(:host).new(host: '192.0.2.1')
transfers = Net::Connector.vendors.filter_map do |vendor|
  profile = Net::Connector.vendor_class(vendor).profile
  next unless profile.tftp_strategy
  filename = vendor == :palo_alto ? 'running-config.xml' : 'backup.cfg'
  target = Net::Connector::TftpTarget.new(host: '192.0.2.9', path: filename)
  source = %i[h3c h3c_wireless huawei].include?(vendor) ? 'flash:/startup.cfg' : nil
  strategy = profile.tftp_strategy.new(context)
  script = strategy.script(target, source_file: source)
  commands = script.map { |c| { text: c.text, timeout: c.timeout, responses: c.interactions.map { |i| i.response("") } } }
  [vendor, { path: filename, source: source, commands: commands }]
end.to_h
require 'net/connector/netdisco/device'
require 'net/connector/netdisco/rules'
require 'net/connector/netdisco/report'
require 'net/connector/device/local_backup'
require 'digest'
raws = ["plain\r\n", "old-progress\rnew\e[K\n", "\e[32mcolor\e[0m\n", "abc\bZ\n", "你好\n", "a\e[5Cb\n"]
renderings = raws.map { |raw| output = Net::Connector::Terminal::Renderer.render(raw).dup.force_encoding(Encoding::UTF_8); {input: raw, output: output, sha256: Digest::SHA256.hexdigest(output)} }
now = Time.utc(2026, 1, 2, 3, 4, 5)
rules = Net::Connector::Netdisco::Rules.new
statuses = %i[backed_up saved_with_error filtered failed]
outcomes = statuses.each_with_index.map do |status, index|
  device = Net::Connector::Netdisco::Device.from_row({'ip' => "192.0.2.#{index+1}", 'name' => "edge#{index+1}", 'vendor' => 'cisco', 'os' => 'ios'}, rules: rules)
  backup = if %i[backed_up saved_with_error].include?(status)
    Net::Connector::Backup.new(path: "/fixtures/#{index+1}.txt", bytes: 5, sha256: Digest::SHA256.hexdigest('hello'), collected_at: now, change: :created)
  end
  failure = %i[saved_with_error failed].include?(status)
  Net::Connector::Netdisco::Outcome.new(device: device, status: status, backup: backup, error_code: failure ? :device_error : nil, error_type: failure ? 'Net::Connector::DeviceError' : nil,
    started_at: status == :filtered ? nil : now, finished_at: status == :filtered ? nil : now + 1, duration_ms: status == :filtered ? nil : 1000)
end
batch = Net::Connector::Netdisco::Batch.new(mode: :backup, outcomes: outcomes, started_at: now, finished_at: now + 2, callback_errors: [], report_location: nil, report_error: nil)
report = Net::Connector::Netdisco::Report.new(batch, policy: :selected, duration_ms: 2000).summary
puts JSON.pretty_generate(profiles: profiles, transfers: transfers, renderings: renderings, report: report)
