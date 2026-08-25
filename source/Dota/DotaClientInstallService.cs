#nullable enable

using System.Text.Json;

namespace SalfetkaHub.Dota;

public sealed class DotaClientInstallService
{
    private const long ExpectedInstallBytes = 90_000_000_000;
    private static readonly JsonSerializerOptions JsonOptions = new(JsonSerializerDefaults.Web)
    {
        PropertyNameCaseInsensitive = true,
    };

    private readonly string statusFile;

    public DotaClientInstallService()
    {
        statusFile = GetEnvironment(
            "DOTA_CLIENT_STATUS_FILE",
            "/app/dota-client-status/status.json");
    }

    public async Task<DotaClientInstallStatus> GetStatusAsync(CancellationToken cancellationToken)
    {
        if (!File.Exists(statusFile))
        {
            return DotaClientInstallStatus.NotStarted(ExpectedInstallBytes);
        }

        try
        {
            await using var stream = new FileStream(
                statusFile,
                FileMode.Open,
                FileAccess.Read,
                FileShare.ReadWrite | FileShare.Delete,
                bufferSize: 16 * 1024,
                useAsync: true);
            return await JsonSerializer.DeserializeAsync<DotaClientInstallStatus>(
                    stream,
                    JsonOptions,
                    cancellationToken)
                ?? DotaClientInstallStatus.Unavailable(
                    ExpectedInstallBytes,
                    "Файл состояния установщика пока пуст.");
        }
        catch (IOException)
        {
            return DotaClientInstallStatus.Unavailable(
                ExpectedInstallBytes,
                "Установщик обновляет состояние. Следующая проверка будет выполнена автоматически.");
        }
        catch (JsonException)
        {
            return DotaClientInstallStatus.Unavailable(
                ExpectedInstallBytes,
                "Получено неполное состояние установщика. Следующая проверка будет выполнена автоматически.");
        }
    }

    private static string GetEnvironment(string name, string fallback)
    {
        var value = Environment.GetEnvironmentVariable(name);
        return string.IsNullOrWhiteSpace(value) ? fallback : value.Trim();
    }
}

public sealed record DotaClientInstallStatus(
    int SchemaVersion,
    bool Available,
    int AppId,
    string Phase,
    string Message,
    double ProgressPercent,
    long DownloadedBytes,
    long DownloadTotalBytes,
    long InstalledBytes,
    long ExpectedInstallBytes,
    long SpeedBytesPerSecond,
    long? EtaSeconds,
    long DiskFreeBytes,
    DateTimeOffset? StartedAt,
    DateTimeOffset? UpdatedAt,
    DateTimeOffset? CompletedAt,
    string? BuildId,
    string? Error,
    IReadOnlyList<string> RecentMessages)
{
    public static DotaClientInstallStatus NotStarted(long expectedInstallBytes) => new(
        1,
        false,
        570,
        "not_started",
        "Установщик клиента ещё не запущен.",
        0,
        0,
        0,
        0,
        expectedInstallBytes,
        0,
        null,
        0,
        null,
        null,
        null,
        null,
        null,
        []);

    public static DotaClientInstallStatus Unavailable(long expectedInstallBytes, string message) => new(
        1,
        false,
        570,
        "unavailable",
        message,
        0,
        0,
        0,
        0,
        expectedInstallBytes,
        0,
        null,
        0,
        null,
        null,
        null,
        null,
        null,
        []);
}
