require "minitest/autorun"
require_relative "seafile_client"

# Configuration via environment variables:
#   SEAFILE_URL      - server base URL (default: http://localhost:8082)
#   SEAFILE_EMAIL    - test user email (default: admin@example.com)
#   SEAFILE_PASSWORD - test user password (required)

module SeafileTestHelper
  def seafile_url
    ENV.fetch("SEAFILE_URL", "http://localhost:8082")
  end

  def seafile_email
    ENV.fetch("SEAFILE_EMAIL", "admin@example.com")
  end

  def seafile_password
    ENV["SEAFILE_PASSWORD"] || raise("SEAFILE_PASSWORD env var is required")
  end

  # Returns a logged-in client. Memoized per test instance.
  def client
    @client ||= begin
      c = SeafileClient.new(seafile_url)
      c.login(seafile_email, seafile_password)
      c
    end
  end

  # Returns an unauthenticated client.
  def anon_client
    @anon_client ||= SeafileClient.new(seafile_url)
  end

  # Creates a repo and ensures it's cleaned up after the test.
  def create_test_repo(name = "test-#{SecureRandom.hex(4)}")
    resp = client.create_repo(name)
    assert resp.ok?, "Failed to create repo: #{resp}"
    repo_id = resp["id"]
    @test_repos ||= []
    @test_repos << repo_id
    repo_id
  end

  def teardown
    (@test_repos || []).each do |repo_id|
      client.delete_repo(repo_id)
    end
  end
end
