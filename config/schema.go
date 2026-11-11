package config

// FieldKind names how a configuration value is decoded and validated.
type FieldKind string

// Field kinds used by configuration decoding.
const (
	KindString          FieldKind = "string"
	KindOptionalString  FieldKind = "optional_string"
	KindInteger         FieldKind = "integer"
	KindOptionalInteger FieldKind = "optional_integer"
	KindNumber          FieldKind = "number"
	KindBoolean         FieldKind = "boolean"
	KindList            FieldKind = "list"
	KindJSON            FieldKind = "json"
	KindJSONArray       FieldKind = "json_array"
)

// Field maps a dotted configuration key to its environment variable and kind.
type Field struct {
	Env  string
	Kind FieldKind
}

var schema = map[string]Field{
	"backup.success_policy":           {Env: "NC_BACKUP_SUCCESS_POLICY", Kind: KindString},
	"tftp.upload_timeout":             {Env: "NC_TFTP_UPLOAD_TIMEOUT", Kind: KindNumber},
	"tftp.archive_prepare_timeout":    {Env: "NC_TFTP_ARCHIVE_PREPARE_TIMEOUT", Kind: KindNumber},
	"tftp.archive_verify_timeout":     {Env: "NC_TFTP_ARCHIVE_VERIFY_TIMEOUT", Kind: KindNumber},
	"tftp.archive_cleanup_timeout":    {Env: "NC_TFTP_ARCHIVE_CLEANUP_TIMEOUT", Kind: KindNumber},
	"backup.layout":                   {Env: "NC_BACKUP_LAYOUT", Kind: KindString},
	"netdisco.cache_directory":        {Env: "ND_CACHE_DIRECTORY", Kind: KindString},
	"netdisco.cache_namespace":        {Env: "ND_CACHE_NAMESPACE", Kind: KindOptionalString},
	"netdisco.cache_ttl":              {Env: "ND_CACHE_TTL", Kind: KindNumber},
	"netdisco.cache_enabled":          {Env: "ND_CACHE_ENABLED", Kind: KindBoolean},
	"netdisco.cache_mode":             {Env: "ND_CACHE_MODE", Kind: KindString},
	"netdisco.source":                 {Env: "ND_SOURCE", Kind: KindString},
	"netdisco.query":                  {Env: "ND_QUERY", Kind: KindString},
	"netdisco.query_params":           {Env: "ND_QUERY_PARAMS", Kind: KindJSONArray},
	"netdisco.url":                    {Env: "ND_URL", Kind: KindString},
	"netdisco.page_size":              {Env: "ND_PAGE_SIZE", Kind: KindInteger},
	"netdisco.max_pages":              {Env: "ND_MAX_PAGES", Kind: KindInteger},
	"netdisco.max_response_bytes":     {Env: "ND_MAX_RESPONSE_BYTES", Kind: KindInteger},
	"netdisco.max_inventory_bytes":    {Env: "ND_MAX_INVENTORY_BYTES", Kind: KindInteger},
	"netdisco.max_devices":            {Env: "ND_MAX_DEVICES", Kind: KindInteger},
	"netdisco.inventory_timeout":      {Env: "ND_INVENTORY_TIMEOUT", Kind: KindNumber},
	"netdisco.allow_insecure_http":    {Env: "ND_ALLOW_INSECURE_HTTP", Kind: KindBoolean},
	"netdisco.db_port":                {Env: "ND_DB_PORT", Kind: KindInteger},
	"netdisco.db_sslmode":             {Env: "ND_DB_SSLMODE", Kind: KindString},
	"netdisco.db_sslrootcert":         {Env: "ND_DB_SSLROOTCERT", Kind: KindString},
	"netdisco.db_connect_timeout":     {Env: "ND_DB_CONNECT_TIMEOUT", Kind: KindInteger},
	"topology.mode":                   {Env: "NC_TOPOLOGY_MODE", Kind: KindString},
	"topology.cache_mode":             {Env: "NC_TOPOLOGY_CACHE_MODE", Kind: KindString},
	"topology.parallel":               {Env: "NC_TOPOLOGY_PARALLEL", Kind: KindInteger},
	"topology.directory":              {Env: "NC_TOPOLOGY_DIRECTORY", Kind: KindString},
	"topology.limit":                  {Env: "NC_TOPOLOGY_LIMIT", Kind: KindOptionalInteger},
	"topology.description_format":     {Env: "NC_TOPOLOGY_DESCRIPTION_FORMAT", Kind: KindString},
	"topology.neighbor_name_prefixes": {Env: "NC_TOPOLOGY_NEIGHBOR_NAME_PREFIXES", Kind: KindList},
	"topology.abbreviate_ports":       {Env: "NC_TOPOLOGY_ABBREVIATE", Kind: KindBoolean},
	"topology.lowercase_ports":        {Env: "NC_TOPOLOGY_LOWERCASE", Kind: KindBoolean},
	"backup.directory":                {Env: "NC_BACKUP_DIRECTORY", Kind: KindString},
	"backup.parallel":                 {Env: "NC_PARALLEL", Kind: KindInteger},
	"backup.limit_per_vendor":         {Env: "NC_SAMPLE_PER_VENDOR", Kind: KindOptionalInteger},
	"inventory.include_hosts":         {Env: "NC_INCLUDE_HOSTS", Kind: KindList},
	"inventory.exclude_hosts":         {Env: "NC_EXCLUDE_HOSTS", Kind: KindList},
	"inventory.include_names":         {Env: "NC_INCLUDE_NAMES", Kind: KindList},
	"inventory.exclude_names":         {Env: "NC_EXCLUDE_NAMES", Kind: KindList},
	"inventory.name_match":            {Env: "NC_NAME_MATCH", Kind: KindString},
	"inventory.include_models":        {Env: "NC_INCLUDE_MODELS", Kind: KindList},
	"inventory.exclude_models":        {Env: "NC_EXCLUDE_MODELS", Kind: KindList},
	"inventory.model_match":           {Env: "NC_MODEL_MATCH", Kind: KindString},
	"inventory.exclude_vendors":       {Env: "NC_EXCLUDE_VENDORS", Kind: KindList},
	"inventory.include_vendors":       {Env: "NC_INCLUDE_VENDORS", Kind: KindList},
	"inventory.vendor_overrides":      {Env: "NC_VENDOR_OVERRIDES", Kind: KindJSON},
	"inventory.host_overrides":        {Env: "NC_HOST_OVERRIDES", Kind: KindJSON},
	"inventory.device_rules":          {Env: "NC_DEVICE_RULES", Kind: KindJSON},
	"ssh.protocol":                    {Env: "NC_PROTOCOL", Kind: KindString},
	"ssh.vendor_protocols":            {Env: "NC_VENDOR_PROTOCOLS", Kind: KindJSON},
	"ssh.max_script_output_bytes":     {Env: "NC_MAX_SCRIPT_OUTPUT_BYTES", Kind: KindOptionalInteger},
	"ssh.known_hosts":                 {Env: "NC_KNOWN_HOSTS", Kind: KindString},
	"ssh.jump_host":                   {Env: "NC_SSH_JUMP_HOST", Kind: KindOptionalString},
	"ssh.connect_retries":             {Env: "NC_SSH_CONNECT_RETRIES", Kind: KindInteger},
	"ssh.host_key_policy":             {Env: "NC_HOST_KEY_POLICY", Kind: KindString},
	"ssh.log_directory":               {Env: "NC_LOG_DIRECTORY", Kind: KindString},
	"ssh.log_level":                   {Env: "NC_LOG_LEVEL", Kind: KindString},
	"tftp.server":                     {Env: "NC_TFTP_HOST", Kind: KindString},
	"tftp.verification_root":          {Env: "NC_TFTP_ROOT", Kind: KindString},
	"tftp.vrfs":                       {Env: "NC_TFTP_VRFS", Kind: KindJSON},
	"tftp.servers":                    {Env: "NC_TFTP_SERVERS", Kind: KindJSON},
	"tftp.verification_roots":         {Env: "NC_TFTP_ROOTS", Kind: KindJSON},
	"tftp.source_interfaces":          {Env: "NC_TFTP_SOURCE_INTERFACES", Kind: KindJSON},
	"tftp.source_files":               {Env: "NC_TFTP_SOURCE_FILES", Kind: KindJSON},
	"tftp.ssh_worker_path":            {Env: "NC_TFTP_SSH_WORKER_PATH", Kind: KindString},
}
