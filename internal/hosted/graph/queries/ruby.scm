(method) @definition
(class) @definition
(module) @definition
(identifier) @reference
(call
  method: (identifier) @import
  (#match? @import "^(require|require_relative|load|autoload)$"))
(call) @call
