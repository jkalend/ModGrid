package dev.modgrid.fixture;

import net.fabricmc.api.DedicatedServerModInitializer;

/** Project-authored fixture. World assertions use Minecraft's saved scoreboard
 * through the owned server console, avoiding a Fabric API/Minecraft compile dependency. */
public final class BootstrapFixture implements DedicatedServerModInitializer {
    @Override
    public void onInitializeServer() {
        System.out.println("MODGRID_AUTHORED_FIXTURE_V1");
    }
}
