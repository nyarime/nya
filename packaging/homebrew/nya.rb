# This file is a template for Homebrew core or a tap (nyarime/nya).
# Submit to homebrew-core after the first stable v1.x tag with adoption docs.
class Nya < Formula
  desc "Archive format built for FEC + distribution (nya get / send)"
  homepage "https://github.com/nyarime/nya"
  license "GPL-3.0-only"
  head "https://github.com/nyarime/nya.git", branch: "main"

  # Replace VERSION on release; sha256 from GitHub release tarball or `brew fetch --force`.
  url "https://github.com/nyarime/nya/archive/refs/tags/v0.1.19.tar.gz"
  sha256 "PLACEHOLDER_RUN_brew_fetch"

  depends_on "go" => :build

  def install
    system "go", "build", "-o", bin/"nya", "./cmd/nya"
    man1.install "docs/man/nya.1" if File.exist?("docs/man/nya.1")
  end

  test do
    system bin/"nya", "help"
    (testpath/"hello.txt").write "nya"
    system bin/"nya", "create", "-profile", "distribute", "test.nya", "hello.txt"
    assert_predicate testpath/"test.nya", :exist?
  end
end
