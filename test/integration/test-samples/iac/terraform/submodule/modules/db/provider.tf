# An aliased provider declared in a sub-directory. lstk's provider discovery
# recurses the working tree, so this block must be represented in the generated
# override even though the root module does not wire this module in.
provider "aws" {
  alias = "replica"
}
