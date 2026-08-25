#nullable enable

using System.Buffers.Binary;
using System.Text;
using System.Text.Json;
using System.Text.RegularExpressions;
using SalfetkaHub.Infrastructure;

namespace SalfetkaHub.Dota;

public sealed class DotaPanelService
{
    private const string ContainerName = "dota-local-hub";
    private static readonly JsonSerializerOptions JsonOptions = new(JsonSerializerDefaults.Web);
    private static readonly Regex AnsiPattern = new(@"\x1B\[[0-?]*[ -/]*[@-~]", RegexOptions.Compiled);

    private readonly IHttpClientFactory httpClientFactory;
    private readonly ServiceObservability observability;
    private readonly string apiBaseUrl;
    private readonly string websiteUrl;
    private readonly string publicWebsiteUrl;
    private readonly string publicPreviewUrl;

    public DotaPanelService(IHttpClientFactory httpClientFactory, ServiceObservabilityFactory observabilityFactory)
    {
        this.httpClientFactory = httpClientFactory;
        observability = observabilityFactory.Create("dota-local-hub", "Dota Local Hub");
        apiBaseUrl = GetEnvironment("DOTA_HUB_API_BASE_URL", "http://dota-local-hub:8787");
        websiteUrl = GetEnvironment("DOTA_HUB_LOCAL_URL", "http://127.0.0.1:8787");
        publicWebsiteUrl = GetEnvironment("DOTA_PUBLIC_URL", "https://fantasy-time.online");
        publicPreviewUrl = GetEnvironment("DOTA_PUBLIC_PREVIEW_URL", "http://127.0.0.1:8790");
    }

    public async Task<DotaPanelStatus> GetStatusAsync(CancellationToken cancellationToken)
    {
        var container = await GetContainerAsync(cancellationToken);
        var health = await GetHealthAsync(container, cancellationToken);
        var monitor = await GetGCMonitorAsync(container, cancellationToken);
        return new DotaPanelStatus(container, health, monitor, websiteUrl, publicWebsiteUrl, publicPreviewUrl);
    }

    public async Task<ServiceHealthSnapshot> GetHealthAsync(CancellationToken cancellationToken)
    {
        var container = await GetContainerAsync(cancellationToken);
        return await GetHealthAsync(container, cancellationToken);
    }

    public Task StartAsync(CancellationToken cancellationToken) =>
        RunContainerActionAsync("start", "Dota Local Hub start was requested.", cancellationToken);

    public Task StopAsync(CancellationToken cancellationToken) =>
        RunContainerActionAsync("stop?t=30", "Dota Local Hub stop was requested.", cancellationToken);

    public Task RestartAsync(CancellationToken cancellationToken) =>
        RunContainerActionAsync("restart?t=30", "Dota Local Hub restart was requested.", cancellationToken);

    public async Task ReloadTournamentAsync(CancellationToken cancellationToken)
    {
        var client = httpClientFactory.CreateClient("internal-api");
        using var response = await client.PostAsync(
            new Uri(new Uri(apiBaseUrl.TrimEnd('/') + "/"), "api/tournament/reload"),
            new StringContent("{}", System.Text.Encoding.UTF8, "application/json"),
            cancellationToken);
        response.EnsureSuccessStatusCode();
        observability.ConfigurationChanged("Dota tournament configuration was reloaded.");
    }

    public async Task<IReadOnlyList<DotaLogEntry>> GetLogsAsync(int take, CancellationToken cancellationToken)
    {
        take = Math.Clamp(take, 20, 500);
        var client = httpClientFactory.CreateClient("docker-api");
        using var response = await client.GetAsync(
            $"/containers/{ContainerName}/logs?stdout=1&stderr=1&timestamps=1&tail={take}",
            cancellationToken);

        if (response.StatusCode == System.Net.HttpStatusCode.NotFound)
        {
            return [];
        }

        response.EnsureSuccessStatusCode();
        var bytes = await response.Content.ReadAsByteArrayAsync(cancellationToken);
        var raw = DecodeDockerLogStream(bytes);
        var result = new List<DotaLogEntry>();

        foreach (var rawLine in raw.Split('\n', StringSplitOptions.RemoveEmptyEntries))
        {
            var line = AnsiPattern.Replace(rawLine.TrimEnd('\r'), "");
            var separator = line.IndexOf(' ');
            var at = DateTimeOffset.UtcNow;
            var message = line;

            if (separator > 0 && DateTimeOffset.TryParse(line[..separator], out var parsed))
            {
                at = parsed;
                message = line[(separator + 1)..];
            }

            result.Add(new DotaLogEntry(at, message));
        }

        return result.TakeLast(take).ToArray();
    }

    private static string DecodeDockerLogStream(byte[] bytes)
    {
        if (bytes.Length < 8 || bytes[1] != 0 || bytes[2] != 0 || bytes[3] != 0)
        {
            return Encoding.UTF8.GetString(bytes);
        }

        var offset = 0;
        var output = new StringBuilder(bytes.Length);
        while (offset + 8 <= bytes.Length)
        {
            var length = BinaryPrimitives.ReadInt32BigEndian(bytes.AsSpan(offset + 4, 4));
            offset += 8;
            if (length < 0 || offset + length > bytes.Length)
            {
                return Encoding.UTF8.GetString(bytes);
            }

            output.Append(Encoding.UTF8.GetString(bytes, offset, length));
            offset += length;
        }

        return output.ToString();
    }

    private async Task<ServiceHealthSnapshot> GetHealthAsync(DotaContainerStatus container, CancellationToken cancellationToken)
    {
        if (!container.Running)
        {
            observability.SetDependency("dota-api", false, "Dota Local Hub container is not running.");
            return observability.Snapshot(false, new Dictionary<string, object?>
            {
                ["containerStatus"] = container.Status,
                ["websiteUrl"] = websiteUrl,
            });
        }

        try
        {
            var client = httpClientFactory.CreateClient("internal-api");
            using var response = await client.GetAsync(
                new Uri(new Uri(apiBaseUrl.TrimEnd('/') + "/"), "api/health"),
                cancellationToken);
            response.EnsureSuccessStatusCode();
            await using var stream = await response.Content.ReadAsStreamAsync(cancellationToken);
            var health = await JsonSerializer.DeserializeAsync<ServiceHealthSnapshot>(stream, JsonOptions, cancellationToken);
            if (health is not null)
            {
                observability.SetDependency("dota-api", true);
                return health;
            }

            throw new InvalidOperationException("Dota Local Hub Health API returned an empty response.");
        }
        catch (Exception ex)
        {
            observability.SetDependency("dota-api", false, ex.Message);
            return observability.Snapshot(true, new Dictionary<string, object?>
            {
                ["containerStatus"] = container.Status,
                ["websiteUrl"] = websiteUrl,
            });
        }
    }

    private async Task<DotaContainerStatus> GetContainerAsync(CancellationToken cancellationToken)
    {
        var client = httpClientFactory.CreateClient("docker-api");
        using var response = await client.GetAsync($"/containers/{ContainerName}/json", cancellationToken);
        if (response.StatusCode == System.Net.HttpStatusCode.NotFound)
        {
            return new DotaContainerStatus(false, false, "not-created", "none", null, 0, "");
        }

        response.EnsureSuccessStatusCode();
        await using var stream = await response.Content.ReadAsStreamAsync(cancellationToken);
        using var document = await JsonDocument.ParseAsync(stream, cancellationToken: cancellationToken);
        var root = document.RootElement;
        var state = root.GetProperty("State");
        var running = state.GetProperty("Running").GetBoolean();
        var status = state.GetProperty("Status").GetString() ?? "unknown";
        var health = state.TryGetProperty("Health", out var healthNode)
            ? healthNode.GetProperty("Status").GetString() ?? "unknown"
            : "none";
        DateTimeOffset? startedAt = null;

        if (state.TryGetProperty("StartedAt", out var startedNode) &&
            DateTimeOffset.TryParse(startedNode.GetString(), out var parsedStartedAt) &&
            parsedStartedAt.Year > 1)
        {
            startedAt = parsedStartedAt;
        }

        var restartCount = root.TryGetProperty("RestartCount", out var restartNode) ? restartNode.GetInt32() : 0;
        var image = root.GetProperty("Config").GetProperty("Image").GetString() ?? "";
        return new DotaContainerStatus(true, running, status, health, startedAt, restartCount, image);
    }

    private async Task<DotaGCMonitorStatus> GetGCMonitorAsync(
        DotaContainerStatus container,
        CancellationToken cancellationToken)
    {
        if (!container.Running)
        {
            return DotaGCMonitorStatus.Offline;
        }

        try
        {
            var client = httpClientFactory.CreateClient("internal-api");
            using var response = await client.GetAsync(
                new Uri(new Uri(apiBaseUrl.TrimEnd('/') + "/"), "api/gc-monitor"),
                cancellationToken);
            response.EnsureSuccessStatusCode();
            await using var stream = await response.Content.ReadAsStreamAsync(cancellationToken);
            return await JsonSerializer.DeserializeAsync<DotaGCMonitorStatus>(stream, JsonOptions, cancellationToken)
                ?? DotaGCMonitorStatus.Offline;
        }
        catch
        {
            return DotaGCMonitorStatus.Offline;
        }
    }

    private async Task RunContainerActionAsync(string action, string eventMessage, CancellationToken cancellationToken)
    {
        var client = httpClientFactory.CreateClient("docker-api");
        using var request = new HttpRequestMessage(HttpMethod.Post, $"/containers/{ContainerName}/{action}");
        using var response = await client.SendAsync(request, cancellationToken);
        if (response.StatusCode is not (System.Net.HttpStatusCode.NoContent or System.Net.HttpStatusCode.NotModified))
        {
            response.EnsureSuccessStatusCode();
        }

        observability.RecordEvent(EventSeverities.Information, "lifecycle", eventMessage);
    }

    private static string GetEnvironment(string name, string fallback)
    {
        var value = Environment.GetEnvironmentVariable(name);
        return string.IsNullOrWhiteSpace(value) ? fallback : value.Trim();
    }
}

public sealed record DotaPanelStatus(
    DotaContainerStatus Server,
    ServiceHealthSnapshot Health,
    DotaGCMonitorStatus Monitor,
    string WebsiteUrl,
    string PublicWebsiteUrl,
    string PublicPreviewUrl);

public sealed record DotaGCMonitorStatus(
    bool Enabled,
    string State,
    string Priority,
    string DiscoverySource,
    DateTimeOffset? LastSuccessfulCycleAt,
    DateTimeOffset? NextCycleAt,
    int ConsecutiveFailures,
    long DiscoveryRequestsTotal,
    long HistoryRequestsTotal,
    long DetailsRequestsTotal,
    long MatchesQueuedTotal,
    int LastCycleCandidates,
    int LastCycleQueued,
    int LastCycleWaitingReplay)
{
    public static DotaGCMonitorStatus Offline { get; } = new(
        false, "offline", "gc-first-metadata", "opendota-index", null, null, 0, 0, 0, 0, 0, 0, 0, 0);
}

public sealed record DotaContainerStatus(
    bool Exists,
    bool Running,
    string Status,
    string Health,
    DateTimeOffset? StartedAt,
    int RestartCount,
    string Image);

public sealed record DotaLogEntry(DateTimeOffset At, string Text);
